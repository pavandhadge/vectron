// This file implements a client for the Placement Driver (PD) service.
// The worker uses this client to register itself, send heartbeats, and receive
// shard assignments from the placement driver.

package pd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	pd "github.com/pavandhadge/vectron/shared/proto/placementdriver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
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

// WorkerID returns the assigned worker ID from the placement driver.
func (c *Client) WorkerID() uint64 {
	return c.workerID
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
		conn, err := grpc.DialContext(ctx, addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                20 * time.Second,
				Timeout:             5 * time.Second,
				PermitWithoutStream: true,
			}),
			grpc.WithInitialWindowSize(1<<20),
			grpc.WithInitialConnWindowSize(1<<20),
			grpc.WithReadBufferSize(64*1024),
			grpc.WithWriteBufferSize(64*1024),
			grpc.WithBlock(),
		)
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
// collectCapacityMetrics gathers system capacity information
func collectCapacityMetrics() (cpuCores int32, memoryBytes, diskBytes int64) {
	// Get CPU cores
	cpuCores = int32(runtime.NumCPU())

	// Get memory info using runtime
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	// Use Sys as total memory allocated from OS (approximation)
	// In production, use gopsutil to get actual system memory
	memoryBytes = int64(m.Sys)

	// Disk capacity - placeholder
	// In production, use syscall to get actual disk size
	diskBytes = 500 * 1024 * 1024 * 1024 // 500GB default

	return
}

// collectFailureDomain gathers failure domain information from environment
// Workers should set these environment variables to enable fault-tolerant placement:
// - VECTRON_RACK: Rack identifier (e.g., "rack-01")
// - VECTRON_ZONE: Availability zone (e.g., "us-east-1a")
// - VECTRON_REGION: Region (e.g., "us-east-1")
func collectFailureDomain() (rack, zone, region string) {
	// In production, these would typically come from:
	// - Kubernetes node labels
	// - Cloud metadata services (EC2, GCE, Azure)
	// - Configuration management
	// - Environment variables (used here for flexibility)

	rack = os.Getenv("VECTRON_RACK")
	if rack == "" {
		rack = "default-rack"
	}

	zone = os.Getenv("VECTRON_ZONE")
	if zone == "" {
		zone = "default-zone"
	}

	region = os.Getenv("VECTRON_REGION")
	if region == "" {
		region = "default-region"
	}

	return
}

func (c *Client) Register(ctx context.Context) error {
	// Collect capacity metrics
	cpuCores, memoryBytes, diskBytes := collectCapacityMetrics()

	// Collect failure domain information
	rack, zone, region := collectFailureDomain()

	req := &pd.RegisterWorkerRequest{
		GrpcAddress: c.grpcAddr,
		RaftAddress: c.raftAddr,
		CpuCores:    cpuCores,
		MemoryBytes: memoryBytes,
		DiskBytes:   diskBytes,
		Rack:        rack,
		Zone:        zone,
		Region:      region,
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

// collectLoadMetrics gathers current load metrics from the worker
func collectLoadMetrics(shardManager ShardManager) (cpuPercent, memoryPercent, diskPercent, qps float32, activeShards int64) {
	// Get CPU usage using runtime statistics
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Memory usage as percentage (simplified - in production, use system memory info)
	// For now, use a placeholder based on allocated memory
	memoryPercent = float32(m.Sys/1024/1024) / 100.0 // Rough estimate in MB
	if memoryPercent > 100 {
		memoryPercent = 100
	}

	// Disk usage - simplified placeholder
	// In production, use syscall to get actual disk usage
	diskPercent = 50.0 // Placeholder

	// Query rate - placeholder
	// In production, track actual query metrics
	qps = 0.0 // Will be populated by shard manager if available

	// Active shards
	if shardManager != nil {
		activeShards = int64(len(shardManager.GetShardLeaderInfo()))
	}

	// CPU usage placeholder - in production, use gopsutil or similar
	cpuPercent = 50.0 // Placeholder

	return
}

// sendHeartbeat sends a single heartbeat to the placement driver.
func (c *Client) sendHeartbeat(ctx context.Context, shardUpdateChan chan<- []*ShardAssignment) {
	if c.workerID == 0 {
		log.Println("Worker not registered yet (workerID is 0), skipping heartbeat.")
		return
	}

	// Collect load metrics
	cpuPercent, memoryPercent, diskPercent, qps, activeShards := collectLoadMetrics(c.shardManager)

	req := &pd.HeartbeatRequest{
		WorkerId:           strconv.FormatUint(c.workerID, 10),
		ShardLeaderInfo:    c.shardManager.GetShardLeaderInfo(),
		Timestamp:          time.Now().Unix(),
		CpuUsagePercent:    cpuPercent,
		MemoryUsagePercent: memoryPercent,
		DiskUsagePercent:   diskPercent,
		QueriesPerSecond:   qps,
		ActiveShards:       activeShards,
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
