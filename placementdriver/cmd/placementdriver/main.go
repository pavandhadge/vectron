package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hashicorp/raft"
	"github.com/pavandhadge/vectron/placementdriver/internal/fsm"
	pdRaft "github.com/pavandhadge/vectron/placementdriver/internal/raft"
	"github.com/pavandhadge/vectron/placementdriver/internal/server"
	pb "github.com/pavandhadge/vectron/placementdriver/proto/placementdriver"
	"google.golang.org/grpc"
)

var (
	grpcAddr  string
	raftAddr  string
	httpAddr  string
	nodeID    string
	dataDir   string
	bootstrap bool
	joinAddr  string
)

func init() {
	flag.StringVar(&grpcAddr, "grpc-addr", "localhost:6001", "gRPC listen address")
	flag.StringVar(&raftAddr, "raft-addr", "localhost:7001", "Raft listen address")
	flag.StringVar(&httpAddr, "http-addr", "localhost:8001", "HTTP listen address for join requests")
	flag.StringVar(&nodeID, "node-id", "", "Node ID")
	flag.StringVar(&dataDir, "data-dir", "data/", "Data directory")
	flag.BoolVar(&bootstrap, "bootstrap", false, "Bootstrap the cluster")
	flag.StringVar(&joinAddr, "join", "", "Join address")
}

func main() {
	flag.Parse()

	if nodeID == "" {
		log.Fatalf("node-id is required")
	}

	dataDir = filepath.Join(dataDir, nodeID)
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		log.Fatalf("failed to create data dir: %v", err)
	}

	// Create the FSM.
	fsm := fsm.NewFSM()

	// Create the Raft node.
	raftNode, err := pdRaft.NewRaft(&pdRaft.Config{
		NodeID:     nodeID,
		ListenAddr: raftAddr,
		DataDir:    dataDir,
		Bootstrap:  bootstrap,
	}, fsm)
	if err != nil {
		log.Fatalf("failed to create raft node: %v", err)
	}

	// Create the gRPC server.
	grpcServer := server.NewServer(raftNode, fsm)

	// Start the gRPC server.
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", grpcAddr, err)
	}
	s := grpc.NewServer()
	pb.RegisterPlacementServiceServer(s, grpcServer)
	go func() {
		log.Printf("gRPC server listening at %v", lis.Addr())
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve gRPC: %v", err)
		}
	}()

	// Start the HTTP server for join requests.
	http.HandleFunc("/join", func(w http.ResponseWriter, r *http.Request) {
		m := make(map[string]string)
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		remoteAddr := m["addr"]
		nodeID := m["id"]

		if raftNode.State() != raft.Leader {
			// Forward the request to the leader.
			leader := raftNode.Leader()
			if leader == "" {
				http.Error(w, "no leader", http.StatusServiceUnavailable)
				return
			}

			// Get leader's http address from its raft address.
			// This is a simplification. A real system would need a more robust discovery mechanism.
			// Here we assume the HTTP port can be derived from the Raft port.
			// A better approach would be to store the HTTP address in the FSM.
			// For now, we'll assume http is raft port + 100
			parts := strings.Split(string(leader), ":")
			if len(parts) != 2 {
				http.Error(w, "invalid leader address", http.StatusInternalServerError)
				return
			}
			port, err := strconv.Atoi(parts[1])
			if err != nil {
				http.Error(w, "invalid leader port", http.StatusInternalServerError)
				return
			}
			leaderHttpAddr := fmt.Sprintf("%s:%d", parts[0], port+1000) // Assuming HTTP is 1000 ports away from raft

			b, err := json.Marshal(m)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			resp, err := http.Post(fmt.Sprintf("http://%s/join", leaderHttpAddr), "application/json", bytes.NewReader(b))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer resp.Body.Close()

			// copy header
			for k, vv := range resp.Header {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(resp.StatusCode)

			io.Copy(w, resp.Body)

			return
		}

		future := raftNode.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(remoteAddr), 0, 0)
		if err := future.Error(); err != nil {
			log.Printf("failed to add voter: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("added voter %s at %s", nodeID, remoteAddr)
	})

	go func() {
		log.Printf("HTTP server listening on %s", httpAddr)
		if err := http.ListenAndServe(httpAddr, nil); err != nil {
			log.Fatalf("failed to start HTTP server: %v", err)
		}
	}()

	// If join address is specified, join the cluster.
	if joinAddr != "" {
		join(joinAddr, raftAddr, nodeID)
	}

	// Wait for a signal to shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("shutting down server...")

	s.GracefulStop()
	shutdownFuture := raftNode.Shutdown()
	if err := shutdownFuture.Error(); err != nil {
		log.Printf("failed to shutdown raft: %v", err)
	}
	log.Println("server stopped")
}

func join(joinAddr, raftAddr, nodeID string) {
	b, err := json.Marshal(map[string]string{"addr": raftAddr, "id": nodeID})
	if err != nil {
		log.Fatalf("failed to marshal join request: %v", err)
	}

	for {
		resp, err := http.Post(fmt.Sprintf("http://%s/join", joinAddr), "application/json", bytes.NewReader(b))
		if err == nil {
			defer resp.Body.Close()
			body, _ := ioutil.ReadAll(resp.Body)
			log.Printf("joined cluster successfully, response: %s", string(body))
			return
		}

		log.Printf("failed to join cluster, will retry in 5s: %v", err)
		time.Sleep(5 * time.Second)
	}
}
