package pd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/pavandhadge/vectron/placementdriver/internal/fsm"
	pd "github.com/pavandhadge/vectron/placementdriver/proto/placementdriver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	heartbeatInterval = 5 * time.Second
)

// Client is a client for the Placement Driver service.
type Client struct {
	grpcClient pd.PlacementServiceClient
	conn       *grpc.ClientConn
	workerID   uint64
	workerAddr string
}

// NewClient creates a new client for the Placement Driver.
func NewClient(pdAddr string, workerAddr string) (*Client, error) {
	conn, err := grpc.NewClient(pdAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	grpcClient := pd.NewPlacementServiceClient(conn)
	return &Client{
		grpcClient: grpcClient,
		conn:       conn,
		workerAddr: workerAddr,
	}, nil
}

// Register registers the worker with the Placement Driver and gets a worker ID.
func (c *Client) Register(ctx context.Context) error {
	req := &pd.RegisterWorkerRequest{
		Address: c.workerAddr,
	}

	res, err := c.grpcClient.RegisterWorker(ctx, req)
	if err != nil {
		return err
	}

	if !res.Success {
		return fmt.Errorf("worker registration failed")
	}

	workerID, err := strconv.ParseUint(res.WorkerId, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse assigned worker ID: %w", err)
	}
	c.workerID = workerID
	log.Printf("Successfully registered with PD. Assigned Worker ID: %d", c.workerID)
	return nil
}

// StartHeartbeatLoop starts a background loop to send heartbeats to the PD.
// It takes a channel to send shard assignment updates on.
func (c *Client) StartHeartbeatLoop(shardUpdateChan chan<- []*fsm.ShardAssignment) {
	log.Println("Starting heartbeat loop...")
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	// Send initial heartbeat immediately.
	c.sendHeartbeat(shardUpdateChan)

	for {
		select {
		case <-ticker.C:
			c.sendHeartbeat(shardUpdateChan)
		}
	}
}

func (c *Client) sendHeartbeat(shardUpdateChan chan<- []*fsm.ShardAssignment) {
	if c.workerID == 0 {
		log.Println("Worker not registered yet, skipping heartbeat.")
		return
	}

	req := &pd.HeartbeatRequest{
		WorkerId: strconv.FormatUint(c.workerID, 10),
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	res, err := c.grpcClient.Heartbeat(ctx, req)
	if err != nil {
		log.Printf("Heartbeat failed: %v", err)
		return
	}

	if !res.Ok {
		log.Printf("Heartbeat response not OK")
		return
	}

	// The response message contains the JSON-encoded shard assignments.
	if res.Message != "" {
		var assignments []*fsm.ShardAssignment
		if err := json.Unmarshal([]byte(res.Message), &assignments); err != nil {
			log.Printf("Failed to unmarshal shard assignments: %v", err)
			return
		}
		log.Printf("Received %d shard assignments from PD.", len(assignments))
		shardUpdateChan <- assignments
	}
}

// Close closes the gRPC connection to the Placement Driver.
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}
