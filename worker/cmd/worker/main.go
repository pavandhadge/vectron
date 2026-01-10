// This file is the main entry point for the Worker service.
// It initializes the worker, registers it with the placement driver,
// manages shard lifecycle, and starts the gRPC server for handling
// data and search operations.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lni/dragonboat/v3"
	"github.com/lni/dragonboat/v3/config"
	"github.com/pavandhadge/vectron/worker/internal"
	"github.com/pavandhadge/vectron/worker/internal/pd"
	"github.com/pavandhadge/vectron/worker/internal/shard"
	worker "github.com/pavandhadge/vectron/shared/proto/worker"
	"google.golang.org/grpc"
)

// Start configures and runs the core components of the worker node.
func Start(nodeID uint64, raftAddr, grpcAddr string, pdAddrs []string, workerDataDir string) {
	// Create a top-level directory for this worker's Raft data.
	nhDataDir := filepath.Join(workerDataDir, fmt.Sprintf("node-%d", nodeID))
	if err := os.MkdirAll(nhDataDir, 0750); err != nil {
		log.Fatalf("failed to create nodehost data dir: %v", err)
	}

	// Configure the raft event listener
	listener := internal.NewLoggingEventListener()

	// Configure and create the Dragonboat NodeHost, which manages all Raft clusters (shards) on this worker.
	nhc := config.NodeHostConfig{
		DeploymentID:      1, // A unique ID for the deployment.
		NodeHostDir:       nhDataDir,
		RaftAddress:       raftAddr,
		ListenAddress:     raftAddr,
		RTTMillisecond:    200,
		RaftEventListener: listener,
	}
	nh, err := dragonboat.NewNodeHost(nhc)
	if err != nil {
		log.Fatalf("failed to create nodehost: %v", err)
	}
	log.Printf("Dragonboat NodeHost created. Node ID: %d, Raft Address: %s", nodeID, raftAddr)

	// Create the shard manager, which is responsible for creating, starting, and stopping shards on this worker.
	shardManager := shard.NewManager(nh, workerDataDir, nodeID)

	// Create a client to communicate with the placement driver.
	pdClient, err := pd.NewClient(pdAddrs, grpcAddr, raftAddr, nodeID, shardManager)
	if err != nil {
		log.Fatalf("failed to create PD client: %v", err)
	}

	// Register the worker with the placement driver, retrying a few times on failure.
	// This call confirms the worker and gets back the final worker ID.
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := pdClient.Register(ctx)
		cancel()
		if err == nil {
			break // Success
		}
		log.Printf("Failed to register with PD (attempt %d): %v. Retrying...", i+1, err)
		if i == 4 {
			log.Fatalf("Could not register with placement driver after multiple attempts.")
		}
		time.Sleep(1 * time.Second)
	}

	// Start the two main background loops for the worker.
	shardUpdateChan := make(chan []*pd.ShardAssignment)
	// 1. The heartbeat loop periodically sends heartbeats to the PD and receives shard assignments.
	go pdClient.StartHeartbeatLoop(shardUpdateChan)
	// 2. The shard synchronization loop processes assignments from the PD.
	go func() {
		for assignments := range shardUpdateChan {
			log.Printf("Received %d shard assignments from PD.", len(assignments))
			shardManager.SyncShards(assignments)
		}
	}()

	// Start the public-facing gRPC server for this worker.
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", grpcAddr, err)
	}
	s := grpc.NewServer()
	worker.RegisterWorkerServiceServer(s, internal.NewGrpcServer(nh, shardManager))
	log.Printf("gRPC server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve gRPC: %v", err)
	}
}

func main() {
	// Define and parse command-line flags.
	var (
		grpcAddr      = flag.String("grpc-addr", "localhost:9090", "gRPC server address")
		raftAddr      = flag.String("raft-addr", "localhost:9191", "Raft communication address")
		pdAddrs       = flag.String("pd-addrs", "localhost:6001", "Comma-separated list of Placement Driver gRPC addresses")
		nodeID        = flag.Uint64("node-id", 1, "Worker Node ID (must be > 0)")
		workerDataDir = flag.String("data-dir", "./worker-data", "Parent directory for all worker data")
	)
	flag.Parse()

	if *nodeID == 0 {
		log.Fatalf("node-id must be > 0")
	}

	// Start the worker in a goroutine.
	pdAddrsList := strings.Split(*pdAddrs, ",")
	go Start(*nodeID, *raftAddr, *grpcAddr, pdAddrsList, *workerDataDir)

	log.Println("Worker started. Waiting for signals.")
	// Wait for an interrupt signal to gracefully shut down.
	sig_chan := make(chan os.Signal, 1)
	signal.Notify(sig_chan, os.Interrupt, syscall.SIGTERM)
	<-sig_chan

	log.Println("Shutting down worker.")
	// Note: A real implementation would need to gracefully stop the NodeHost and gRPC server.
}
