// This file is the main entry point for the Placement Driver (PD) service.
// It parses command-line flags, initializes the Raft node, and starts the gRPC server
// that exposes the PlacementService. It is responsible for the lifecycle of a single
// PD node.

package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pavandhadge/vectron/placementdriver/internal/graceful"
	pdRaft "github.com/pavandhadge/vectron/placementdriver/internal/raft"
	"github.com/pavandhadge/vectron/placementdriver/internal/server"
	pb "github.com/pavandhadge/vectron/shared/proto/placementdriver"
	"google.golang.org/grpc"
)

// Command-line flags
var (
	grpcAddr          string // gRPC listen address
	raftAddr          string // Raft listen address
	nodeID            uint64 // Unique ID for this node in the Raft cluster
	clusterID         uint64 // Unique ID for the Raft cluster
	initialMembersStr string // Comma-separated list of initial members for bootstrapping
	dataDir           string // Directory to store Raft data
)

// init registers and parses the command-line flags.
func init() {
	flag.StringVar(&grpcAddr, "grpc-addr", "localhost:6001", "gRPC listen address")
	flag.StringVar(&raftAddr, "raft-addr", "localhost:7001", "Raft listen address")
	flag.Uint64Var(&nodeID, "node-id", 1, "Node ID (must be > 0)")
	flag.Uint64Var(&clusterID, "cluster-id", 1, "Cluster ID")
	flag.StringVar(&initialMembersStr, "initial-members", "1:localhost:7001", "Comma-separated list of initial cluster members, e.g., '1:host1:7001,2:host2:7002'")
	flag.StringVar(&dataDir, "data-dir", "pd-data", "Data directory")
}

// Start configures and runs the core components of the placement driver node.
func Start(nodeID, clusterID uint64, raftAddr, grpcAddr, dataDir string, initialMembers map[uint64]string, shutdownHandler *graceful.ShutdownHandler) {
	// Create a dedicated directory for this node's data.
	nodeDataDir := filepath.Join(dataDir, fmt.Sprintf("node-%d", nodeID))
	if err := os.MkdirAll(nodeDataDir, 0750); err != nil {
		log.Fatalf("failed to create data dir: %v", err)
	}

	// Configure and create the underlying Raft node.
	raftConfig := pdRaft.Config{
		NodeID:         nodeID,
		ClusterID:      clusterID,
		RaftAddress:    raftAddr,
		InitialMembers: initialMembers,
		DataDir:        nodeDataDir,
	}
	raftNode, err := pdRaft.NewNode(raftConfig)
	if err != nil {
		log.Fatalf("failed to create raft node: %v", err)
	}

	// Register Raft node for graceful shutdown
	shutdownHandler.Register(graceful.NewRaftNode(func() { raftNode.Stop() }, "Raft Node"))

	// The FSM (Finite State Machine) holds the application state (workers, collections, etc.).
	// It is managed by the Raft node.
	fsm := raftNode.GetFSM()
	if fsm == nil {
		log.Fatalf("failed to get FSM from raft node")
	}

	// Create the gRPC server, which provides the PlacementService API.
	// Add timeout interceptor for all requests
	grpcServer := server.NewServer(raftNode, fsm)
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", grpcAddr, err)
	}

	// Create gRPC server with timeout interceptor
	s := grpc.NewServer(
		grpc.UnaryInterceptor(graceful.TimeoutInterceptor(30*time.Second)),
		grpc.StreamInterceptor(graceful.TimeoutInterceptorStream(30*time.Second)),
	)
	pb.RegisterPlacementServiceServer(s, grpcServer)

	// Register gRPC server for graceful shutdown
	shutdownHandler.Register(graceful.NewGRPCServer(s, grpcAddr))

	// Start the reconciler for automatic shard management
	reconciler := server.NewReconciler(grpcServer, server.DefaultReconciliationConfig())
	reconciler.Start()

	// Register reconciler for graceful shutdown
	shutdownHandler.Register(graceful.NewReconciler(func() { reconciler.Stop() }, "Shard Reconciler"))

	log.Printf("gRPC server listening at %v", lis.Addr())

	// Start serving in a goroutine
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve gRPC: %v", err)
		}
	}()
}

func main() {
	flag.Parse()

	if nodeID == 0 {
		log.Fatalf("node-id is required and must be > 0")
	}

	initialMembers, err := parseInitialMembers(initialMembersStr)
	if err != nil {
		log.Fatalf("failed to parse initial members: %v", err)
	}

	// Create graceful shutdown handler with 30 second timeout
	shutdownHandler := graceful.NewShutdownHandler(30 * time.Second)
	shutdownHandler.Listen()

	// Start the server
	Start(nodeID, clusterID, raftAddr, grpcAddr, dataDir, initialMembers, shutdownHandler)

	// Block forever (shutdown handler will handle signals)
	select {}
}

// parseInitialMembers converts the comma-separated string of initial members
// into a map of node IDs to their Raft addresses.
// It expects the string to be in the format: "id1:host1:port1,id2:host2:port2,..."
func parseInitialMembers(s string) (map[uint64]string, error) {
	members := make(map[uint64]string)
	if s == "" {
		return members, nil
	}

	parts := strings.Split(s, ",")
	for _, part := range parts {
		p := strings.Split(strings.TrimSpace(part), ":")
		if len(p) != 3 { // Strictly enforce "id:host:port" format
			return nil, fmt.Errorf("invalid member format, expected 'id:host:port', got: '%s'", part)
		}

		id, err := strconv.ParseUint(p[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse node id '%s': %w", p[0], err)
		}
		if id == 0 {
			return nil, fmt.Errorf("node id cannot be 0")
		}

		addr := p[1] + ":" + p[2]
		members[id] = addr
	}
	return members, nil
}
