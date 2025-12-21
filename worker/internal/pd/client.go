// This file implements a client for the Placement Driver (PD) service.
// The worker uses this client to register itself, send heartbeats, and receive
// shard assignments from the placement driver.

package pd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	pd "github.com/pavandhadge/vectron/worker/proto/placementdriver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// heartbeatInterval is the interval at which the worker sends heartbeats to the PD.
	heartbeatInterval = 5 * time.Second
)

// ShardManager defines the interface for the shard manager.
type ShardManager interface {
	GetShardLeaderInfo() []*pd.ShardLeaderInfo
}

// LeaderInfo holds the information about the current leader.
type LeaderInfo struct {
	grpcClient pd.PlacementServiceClient
	conn       *grpc.ClientConn
	address    string
}

// Client is a gRPC client for the Placement Driver service.
type Client struct {
	pdAddrs      []string
	leader       *LeaderInfo
	leaderMu     sync.RWMutex
	workerID     uint64 // The ID assigned by the PD after registration.
	grpcAddr     string // The gRPC address of this worker.
	raftAddr     string // The Raft address of this worker.
	shardManager ShardManager
}

func (c *Client) getLeader() (*LeaderInfo, error) {
	c.leaderMu.RLock()
	defer c.leaderMu.RUnlock()
	if c.leader == nil {
		return nil, fmt.Errorf("no leader available")
	}
	return c.leader, nil
}

func (c *Client) updateLeader(ctx context.Context) error {
	c.leaderMu.Lock()
	defer c.leaderMu.Unlock()

	for _, addr := range c.pdAddrs {
		// Try to connect to a PD node
		conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
		if err != nil {
			log.Printf("Failed to connect to PD node at %s: %v", addr, err)
			continue
		}

		grpcClient := pd.NewPlacementServiceClient(conn)
		// Use ListWorkers as a way to check for leadership.
		// A more robust solution would be a dedicated GetLeader RPC.
		_, err = grpcClient.ListWorkers(ctx, &pd.ListWorkersRequest{})
		if err != nil {
			log.Printf("Failed to get leader from PD node at %s: %v", addr, err)
			conn.Close()
			continue
		}

		log.Printf("Successfully discovered and connected to PD leader at %s", addr)
		if c.leader != nil {
			c.leader.conn.Close()
		}
		c.leader = &LeaderInfo{
			grpcClient: grpcClient,
			conn:       conn,
			address:    addr,
		}
		return nil

	}

	return fmt.Errorf("could not find leader in any of the provided PD addresses")
}

// NewClient creates a new client for the Placement Driver.
func NewClient(pdAddrs []string, grpcAddr, raftAddr string, workerID uint64, shardManager ShardManager) (*Client, error) {
	c := &Client{
		pdAddrs:      pdAddrs,
		grpcAddr:     grpcAddr,
		raftAddr:     raftAddr,
		workerID:     workerID,
		shardManager: shardManager,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.updateLeader(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize connection with PD leader: %w", err)
	}

	return c, nil
}

// Register registers the worker with the Placement Driver.
// The PD will assign a unique ID to the worker, which may be different from the
// node ID provided at startup, although they are the same in this implementation.
func (c *Client) Register(ctx context.Context) error {
	req := &pd.RegisterWorkerRequest{
		GrpcAddress: c.grpcAddr,
		RaftAddress: c.raftAddr,
	}

	leader, err := c.getLeader()
	if err != nil {
		return fmt.Errorf("could not get PD leader: %w", err)
	}

	res, err := leader.grpcClient.RegisterWorker(ctx, req)
	if err != nil {
		log.Printf("Failed to register with current leader: %v. Attempting to find new leader.", err)
		if err := c.updateLeader(ctx); err != nil {
			return fmt.Errorf("failed to update PD leader after registration failure: %w", err)
		}
		leader, err := c.getLeader()
		if err != nil {
			return fmt.Errorf("could not get new PD leader: %w", err)
		}
		res, err = leader.grpcClient.RegisterWorker(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to register worker even after leader update: %w", err)
		}
	}

	if !res.Success {
		return fmt.Errorf("worker registration was not successful")
	}

	assignedID, err := strconv.ParseUint(res.WorkerId, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse assigned worker ID from PD: %w", err)
	}
	c.workerID = assignedID
	log.Printf("Successfully registered with PD. Assigned Worker ID: %d", c.workerID)
	return nil
}

// StartHeartbeatLoop starts a background loop that periodically sends heartbeats to the PD.
// It runs indefinitely until the worker process is terminated.
// It sends shard assignment updates received from the PD on the provided channel.
func (c *Client) StartHeartbeatLoop(shardUpdateChan chan<- []*ShardAssignment) {
	log.Println("Starting heartbeat loop...")
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	// Send the first heartbeat immediately upon starting.
	c.sendHeartbeat(context.Background(), shardUpdateChan)

	for {
		select {
		case <-ticker.C:
			c.sendHeartbeat(context.Background(), shardUpdateChan)
		}
	}
}

// sendHeartbeat sends a single heartbeat to the placement driver.
func (c *Client) sendHeartbeat(ctx context.Context, shardUpdateChan chan<- []*ShardAssignment) {
	if c.workerID == 0 {
		log.Println("Worker not registered yet (workerID is 0), skipping heartbeat.")
		return
	}

	req := &pd.HeartbeatRequest{
		WorkerId:        strconv.FormatUint(c.workerID, 10),
		ShardLeaderInfo: c.shardManager.GetShardLeaderInfo(),
	}

	leader, err := c.getLeader()
	if err != nil {
		log.Printf("Cannot send heartbeat: no leader available. Trying to find one...")
		if err := c.updateLeader(ctx); err != nil {
			log.Printf("Failed to find a new leader: %v", err)
			return
		}
		leader, _ = c.getLeader()
	}

	res, err := leader.grpcClient.Heartbeat(ctx, req)
	if err != nil {
		log.Printf("Heartbeat failed for leader %s: %v. Attempting to find new leader.", leader.address, err)
		if err := c.updateLeader(ctx); err != nil {
			log.Printf("Failed to find new leader after heartbeat failure: %v", err)
			return
		}
		// Retry heartbeat with the new leader
		newLeader, err := c.getLeader()
		if err != nil {
			log.Printf("Failed to get new leader for retry: %v", err)
			return
		}
		res, err = newLeader.grpcClient.Heartbeat(ctx, req)
		if err != nil {
			log.Printf("Heartbeat retry failed for new leader %s: %v", newLeader.address, err)
			return
		}
	}

	if !res.Ok {
		log.Printf("Heartbeat response was not OK. PD might consider this worker dead.")
		return
	}

	// The response message contains a JSON-encoded list of shard assignments.
	if res.Message != "" {
		var assignments []*ShardAssignment
		if err := json.Unmarshal([]byte(res.Message), &assignments); err != nil {
			log.Printf("Failed to unmarshal shard assignments from PD: %v", err)
			return
		}
		if len(assignments) > 0 {
			log.Printf("Received %d shard assignments from PD.", len(assignments))
			shardUpdateChan <- assignments
		}
	}
}

// Close closes the gRPC connection to the Placement Driver.
func (c *Client) Close() {
	c.leaderMu.RLock()
	defer c.leaderMu.RUnlock()
	if c.leader != nil && c.leader.conn != nil {
		c.leader.conn.Close()
	}
}
