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
	"runtime"
	rpprof "runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lni/dragonboat/v3"
	"github.com/lni/dragonboat/v3/config"
	worker "github.com/pavandhadge/vectron/shared/proto/worker"
	"github.com/pavandhadge/vectron/shared/runtimeutil"
	"github.com/pavandhadge/vectron/worker/internal"
	"github.com/pavandhadge/vectron/worker/internal/pd"
	"github.com/pavandhadge/vectron/worker/internal/shard"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
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

	// Ensure shard manager uses the PD-assigned worker ID for raft membership.
	if assignedID := pdClient.WorkerID(); assignedID > 0 {
		shardManager.SetNodeID(assignedID)
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
	maxRecv := envIntDefault("GRPC_MAX_RECV_MB", 256) * 1024 * 1024
	maxSend := envIntDefault("GRPC_MAX_SEND_MB", 256) * 1024 * 1024
	maxStreams := envIntDefault("GRPC_MAX_STREAMS", 1024)
	s := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.ReadBufferSize(64*1024),
		grpc.WriteBufferSize(64*1024),
		grpc.MaxConcurrentStreams(uint32(maxStreams)),
		grpc.MaxRecvMsgSize(maxRecv),
		grpc.MaxSendMsgSize(maxSend),
	)
	worker.RegisterWorkerServiceServer(s, internal.NewGrpcServer(nh, shardManager))
	log.Printf("gRPC server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve gRPC: %v", err)
	}
}

func main() {
	runtimeutil.ConfigureGOMAXPROCS("worker")
	startSelfDumpProfiles("worker")
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

func startSelfDumpProfiles(role string) {
	if v := envInt("PPROF_MUTEX_FRACTION"); v > 0 {
		runtime.SetMutexProfileFraction(v)
	}
	if v := envInt("PPROF_BLOCK_RATE"); v > 0 {
		runtime.SetBlockProfileRate(v)
	}

	cpuPath := os.Getenv("PPROF_CPU_PATH")
	if cpuPath != "" {
		_ = os.MkdirAll(filepath.Dir(cpuPath), 0755)
		if f, err := os.Create(cpuPath); err == nil {
			if err := rpprof.StartCPUProfile(f); err == nil {
				seconds := envIntDefault("PPROF_CPU_SECONDS", 15)
				go func() {
					time.Sleep(time.Duration(seconds) * time.Second)
					rpprof.StopCPUProfile()
					_ = f.Close()
					log.Printf("%s cpu profile -> %s", role, cpuPath)
				}()
			} else {
				_ = f.Close()
			}
		}
	}

	mutexPath := os.Getenv("PPROF_MUTEX_PATH")
	if mutexPath != "" {
		_ = os.MkdirAll(filepath.Dir(mutexPath), 0755)
		seconds := envIntDefault("PPROF_MUTEX_SECONDS", envIntDefault("PPROF_CPU_SECONDS", 15))
		go func() {
			time.Sleep(time.Duration(seconds) * time.Second)
			if f, err := os.Create(mutexPath); err == nil {
				_ = rpprof.Lookup("mutex").WriteTo(f, 0)
				_ = f.Close()
				log.Printf("%s mutex profile -> %s", role, mutexPath)
			}
		}()
	}

	blockPath := os.Getenv("PPROF_BLOCK_PATH")
	if blockPath != "" {
		_ = os.MkdirAll(filepath.Dir(blockPath), 0755)
		seconds := envIntDefault("PPROF_BLOCK_SECONDS", envIntDefault("PPROF_CPU_SECONDS", 15))
		go func() {
			time.Sleep(time.Duration(seconds) * time.Second)
			if f, err := os.Create(blockPath); err == nil {
				_ = rpprof.Lookup("block").WriteTo(f, 0)
				_ = f.Close()
				log.Printf("%s block profile -> %s", role, blockPath)
			}
		}()
	}
}

func envInt(key string) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

func envIntDefault(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
