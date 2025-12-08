package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	pdRaft "github.com/pavandhadge/vectron/placementdriver/internal/raft"
	"github.com/pavandhadge/vectron/placementdriver/internal/server"
	pb "github.com/pavandhadge/vectron/placementdriver/proto/placementdriver"
	"google.golang.org/grpc"
)

var (
	grpcAddr          string
	raftAddr          string
	nodeID            uint64
	clusterID         uint64
	initialMembersStr string
	dataDir           string
)

func init() {
	flag.StringVar(&grpcAddr, "grpc-addr", "localhost:6001", "gRPC listen address")
	flag.StringVar(&raftAddr, "raft-addr", "localhost:7001", "Raft listen address")
	flag.Uint64Var(&nodeID, "node-id", 1, "Node ID (must be > 0)")
	flag.Uint64Var(&clusterID, "cluster-id", 1, "Cluster ID")
	flag.StringVar(&initialMembersStr, "initial-members", "1:localhost:7001", "Comma-separated list of initial cluster members, e.g., '1:host1:7001,2:host2:7002'")
	flag.StringVar(&dataDir, "data-dir", "pd-data", "Data directory")
}

func Start(nodeID uint64, clusterID uint64, raftAddr, grpcAddr, dataDir string, initialMembers map[uint64]string) {
	// Create the data directory.
	nodeDataDir := filepath.Join(dataDir, fmt.Sprintf("node-%d", nodeID))
	if err := os.MkdirAll(nodeDataDir, 0750); err != nil {
		log.Fatalf("failed to create data dir: %v", err)
	}

	// Configure and create the Raft node.
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
	// Note: In a real test, we would need a way to stop this.
	// For now, we assume the test process will be killed.

	// Get the FSM instance from the Raft node.
	fsm := raftNode.GetFSM()
	if fsm == nil {
		log.Fatalf("failed to get FSM from raft node")
	}

	// Create and start the gRPC server.
	grpcServer := server.NewServer(raftNode, fsm)
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", grpcAddr, err)
	}
	s := grpc.NewServer()
	pb.RegisterPlacementServiceServer(s, grpcServer)
	log.Printf("gRPC server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve gRPC: %v", err)
	}
}

func main() {
	flag.Parse()

	if nodeID == 0 {
		log.Fatalf("node-id is required and must be > 0")
	}

	// Parse the initial members flag.
	initialMembers, err := parseInitialMembers(initialMembersStr)
	if err != nil {
		log.Fatalf("failed to parse initial members: %v", err)
	}

	go Start(nodeID, clusterID, raftAddr, grpcAddr, dataDir, initialMembers)

	// Wait for a signal to shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("shutting down server...")
}

// parseInitialMembers parses the -initial-members flag string.
func parseInitialMembers(s string) (map[uint64]string, error) {
	members := make(map[uint64]string)
	if s == "" {
		return members, nil
	}

	parts := strings.Split(s, ",")
	for _, part := range parts {
		p := strings.Split(part, ":")
		if len(p) != 2 && len(p) != 3 { // Allow for host:port or id:host:port
			return nil, fmt.Errorf("invalid member format: %s", part)
		}

		var idStr, addr string
		if len(p) == 2 { // host:port format, assume id is same as what we're running
			idStr = fmt.Sprintf("%d", nodeID)
			addr = p[0] + ":" + p[1]
		} else { // id:host:port
			idStr = p[0]
			addr = p[1] + ":" + p[2]
		}

		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse node id '%s': %w", idStr, err)
		}
		if id == 0 {
			return nil, fmt.Errorf("node id cannot be 0")
		}
		members[id] = addr
	}
	return members, nil
}
