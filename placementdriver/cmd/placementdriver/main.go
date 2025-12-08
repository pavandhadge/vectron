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
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
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

	// If bootstrap is enabled, we are the first node.
	// We need to wait until we are the leader, then register ourselves.
	if bootstrap {
		go func() {
			for {
				time.Sleep(1 * time.Second)
				if raftNode.State() == raft.Leader {
					if err := registerPeer(raftNode, nodeID, raftAddr, httpAddr); err != nil {
						log.Fatalf("failed to register self: %v", err)
					}
					return
				}
			}
		}()
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

		remoteRaftAddr := m["addr"]
		remoteNodeID := m["id"]
		remoteAPIAddr := m["api_addr"]

		if raftNode.State() != raft.Leader {
			leaderRaftAddr := raftNode.Leader()
			if leaderRaftAddr == "" {
				http.Error(w, "no leader", http.StatusServiceUnavailable)
				return
			}

			// Get leader's ID from its address
			var leaderID string
			cf := raftNode.GetConfiguration()
			if err := cf.Error(); err != nil {
				http.Error(w, "failed to get raft configuration", http.StatusInternalServerError)
				return
			}
			for _, srv := range cf.Configuration().Servers {
				if srv.Address == leaderRaftAddr {
					leaderID = string(srv.ID)
					break
				}
			}
			if leaderID == "" {
				http.Error(w, "leader not found in configuration", http.StatusInternalServerError)
				return
			}

			// Get leader's http address from FSM
			peer, ok := fsm.GetPeer(leaderID)
			if !ok {
				http.Error(w, "leader not ready", http.StatusServiceUnavailable)
				return
			}

			// Redirect to the leader.
			redirectURL := "http://" + peer.APIAddr + "/join"
			log.Printf("redirecting join request to leader at %s", redirectURL)
			http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
			return
		}

		// This node is the leader. Add the new node as a voter.
		future := raftNode.AddVoter(raft.ServerID(remoteNodeID), raft.ServerAddress(remoteRaftAddr), 0, 0)
		if err := future.Error(); err != nil {
			log.Printf("failed to add voter: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("added voter %s at %s", remoteNodeID, remoteRaftAddr)

		// Propose a command to register the new peer.
		if err := registerPeer(raftNode, remoteNodeID, remoteRaftAddr, remoteAPIAddr); err != nil {
			log.Printf("failed to register peer %s: %v", remoteNodeID, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			// At this point, the voter is added but peer registration failed.
			// This might lead to inconsistencies. A real production system would need
			// a more robust way to handle this, e.g. retries or a cleanup mechanism.
			return
		}

		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "OK")
	})

	go func() {
		log.Printf("HTTP server listening on %s", httpAddr)
		if err := http.ListenAndServe(httpAddr, nil); err != nil {
			log.Fatalf("failed to start HTTP server: %v", err)
		}
	}()

	// If join address is specified, join the cluster.
	if joinAddr != "" {
		go func() {
			join(joinAddr, raftAddr, httpAddr, nodeID)
			if err := registerPeer(raftNode, nodeID, raftAddr, httpAddr); err != nil {
				log.Fatalf("failed to register self after joining: %v", err)
			}
		}()
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

func join(joinAddr, raftAddr, apiAddr, nodeID string) {
	b, err := json.Marshal(map[string]string{"addr": raftAddr, "api_addr": apiAddr, "id": nodeID})
	if err != nil {
		log.Fatalf("failed to marshal join request: %v", err)
	}

	// Create a client that does not follow redirects automatically for POST requests.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for {
		// The joinAddr might be just host:port, so we need to add http://
		if !strings.HasPrefix(joinAddr, "http") {
			joinAddr = "http://" + joinAddr
		}

		u, err := url.Parse(joinAddr)
		if err != nil {
			log.Fatalf("invalid join address: %v", err)
		}
		u.Path = "/join"

		req, err := http.NewRequest("POST", u.String(), bytes.NewReader(b))
		if err != nil {
			log.Printf("failed to create join request: %v, will retry in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("failed to join cluster, will retry in 5s: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			body, _ := ioutil.ReadAll(resp.Body)
			log.Printf("joined cluster successfully, response: %s", string(body))
			return
		}

		if resp.StatusCode == http.StatusTemporaryRedirect {
			location := resp.Header.Get("Location")
			if location == "" {
				log.Printf("join redirect location is empty, will retry with the same address in 5s")
			} else {
				log.Printf("redirecting join request to %s", location)
				joinAddr = location
			}
			time.Sleep(1 * time.Second) // small delay before retrying redirect
			continue
		}

		body, _ := ioutil.ReadAll(resp.Body)
		log.Printf("failed to join cluster with status %d: %s, will retry in 5s", resp.StatusCode, string(body))
		time.Sleep(5 * time.Second)
	}
}

func registerPeer(raftNode *pdRaft.Raft, nodeID, raftAddr, apiAddr string) error {
	payload := fsm.RegisterPeerPayload{
		ID:       nodeID,
		RaftAddr: raftAddr,
		APIAddr:  apiAddr,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal register peer payload: %w", err)
	}
	cmd := &fsm.Command{
		Type:    fsm.RegisterPeer,
		Payload: payloadBytes,
	}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal register peer command: %w", err)
	}

	applyFuture := raftNode.Apply(cmdBytes, 5*time.Second)
	if err := applyFuture.Error(); err != nil {
		return fmt.Errorf("failed to apply register peer command: %w", err)
	}
	log.Printf("registered peer %s with api address %s", nodeID, apiAddr)
	return nil
}
