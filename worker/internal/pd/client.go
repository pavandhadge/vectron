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
	"time"

	pd "github.com/pavandhadge/vectron/worker/proto/placementdriver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// heartbeatInterval is the interval at which the worker sends heartbeats to the PD.
	heartbeatInterval = 5 * time.Second
)

// Client is a gRPC client for the Placement Driver service.
type Client struct {
	grpcClient pd.PlacementServiceClient
	conn       *grpc.ClientConn
	workerID   uint64 // The ID assigned by the PD after registration.
	grpcAddr   string // The gRPC address of this worker.
	raftAddr   string // The Raft address of this worker.
}

// NewClient creates a new client for the Placement Driver.
func NewClient(pdAddr, grpcAddr, raftAddr string, workerID uint64) (*Client, error) {
	conn, err := grpc.Dial(pdAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to placement driver at %s: %w", pdAddr, err)
	}

	grpcClient := pd.NewPlacementServiceClient(conn)
	return &Client{
		grpcClient: grpcClient,
		conn:       conn,
		grpcAddr:   grpcAddr,
		raftAddr:   raftAddr,
		workerID:   workerID, // Initially, this might be the same as the node ID from flags.
	}, nil
}

// Register registers the worker with the Placement Driver.
// The PD will assign a unique ID to the worker, which may be different from the
// node ID provided at startup, although they are the same in this implementation.
func (c *Client) Register(ctx context.Context) error {
	req := &pd.RegisterWorkerRequest{
		GrpcAddress: c.grpcAddr,
		RaftAddress: c.raftAddr,
	}

	res, err := c.grpcClient.RegisterWorker(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to register worker: %w", err)
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
	c.sendHeartbeat(shardUpdateChan)

	for {
		<-ticker.C
		c.sendHeartbeat(shardUpdateChan)
	}
}

// sendHeartbeat sends a single heartbeat to the placement driver.
func (c *Client) sendHeartbeat(shardUpdateChan chan<- []*ShardAssignment) {
	if c.workerID == 0 {
		log.Println("Worker not registered yet (workerID is 0), skipping heartbeat.")
		return
	}

	req := &pd.HeartbeatRequest{
		WorkerId: strconv.FormatUint(c.workerID, 10),
		// In a real system, you would include stats like vector count, memory usage, etc.
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := c.grpcClient.Heartbeat(ctx, req)
	if err != nil {
		log.Printf("Heartbeat failed: %v", err)
		return
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
	if c.conn != nil {
		c.conn.Close()
	}
}
