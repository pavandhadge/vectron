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
	"syscall"
	"time"

	"github.com/lni/dragonboat/v3"
	"github.com/lni/dragonboat/v3/config"
	"github.com/pavandhadge/vectron/worker/internal"
	"github.com/pavandhadge/vectron/worker/internal/pd"
	"github.com/pavandhadge/vectron/worker/internal/shard"
	"github.com/pavandhadge/vectron/worker/proto/worker"
	"google.golang.org/grpc"
)

func Start(nodeID uint64, raftAddr, grpcAddr, pdAddr, workerDataDir string) {
	// Create the top-level directory for the NodeHost.
	nhDataDir := filepath.Join(workerDataDir, fmt.Sprintf("node-%d", nodeID))
	if err := os.MkdirAll(nhDataDir, 0750); err != nil {
		log.Fatalf("failed to create nodehost data dir: %v", err)
	}

	// Configure and create the NodeHost.
	nhc := config.NodeHostConfig{
		DeploymentID:   1,
		NodeHostDir:    nhDataDir,
		RaftAddress:    raftAddr,
		ListenAddress:  raftAddr,
		RTTMillisecond: 200,
	}
	nh, err := dragonboat.NewNodeHost(nhc)
	if err != nil {
		log.Fatalf("failed to create nodehost: %v", err)
	}
	// Note: In a real test, we would need a way to stop this.

	log.Printf("Dragonboat NodeHost created. Node ID: %d, Raft Address: %s", nodeID, raftAddr)

	// Create client for Placement Driver
	pdClient, err := pd.NewClient(pdAddr, grpcAddr, raftAddr)
	if err != nil {
		log.Fatalf("failed to create PD client: %v", err)
	}

	// Register the worker with the PD.
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if err := pdClient.Register(ctx); err == nil {
			break
		}
	}

	// Create the shard manager.
	shardManager := shard.NewManager(nh, workerDataDir, nodeID)

	// Start the heartbeat loop and shard assignment processing.
	shardUpdateChan := make(chan []*pd.ShardAssignment)
	go pdClient.StartHeartbeatLoop(shardUpdateChan)

	go func() {
		for assignments := range shardUpdateChan {
			shardManager.SyncShards(assignments)
		}
	}()

	// Start the gRPC server.
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
	var (
		grpcAddr      = flag.String("grpc-addr", "localhost:9090", "gRPC server address")
		raftAddr      = flag.String("raft-addr", "localhost:9191", "Raft communication address")
		pdAddr        = flag.String("pd-addr", "localhost:6001", "Placement Driver gRPC address")
		nodeID        = flag.Uint64("node-id", 1, "Worker Node ID (must be > 0)")
		workerDataDir = flag.String("data-dir", "./worker-data", "Parent directory for all worker data")
	)
	flag.Parse()

	if *nodeID == 0 {
		log.Fatalf("node-id must be > 0")
	}

	go Start(*nodeID, *raftAddr, *grpcAddr, *pdAddr, *workerDataDir)

	log.Println("Worker started. Waiting for signals.")
	sig_chan := make(chan os.Signal, 1)
	signal.Notify(sig_chan, os.Interrupt, syscall.SIGTERM)
	<-sig_chan

	log.Println("Shutting down worker.")
}
