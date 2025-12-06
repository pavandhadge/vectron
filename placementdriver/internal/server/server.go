package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/pavandhadge/vectron/placementdriver/internal/fsm"
	"github.com/pavandhadge/vectron/placementdriver/internal/raft"
	pb "github.com/pavandhadge/vectron/placementdriver/proto/placementdriver"
)

// Server implements the PlacementService gRPC service.
type Server struct {
	pb.UnimplementedPlacementServiceServer
	raft *raft.Raft
	fsm  *fsm.FSM
}

// NewServer creates a new Server.
func NewServer(r *raft.Raft, f *fsm.FSM) *Server {
	return &Server{
		raft: r,
		fsm:  f,
	}
}

// RegisterWorker handles a worker registration request.
func (s *Server) RegisterWorker(ctx context.Context, req *pb.RegisterWorkerRequest) (*pb.RegisterWorkerResponse, error) {
	workerID := uuid.New().String()

	payload := fsm.RegisterWorkerPayload{
		ID:      workerID,
		Address: req.Address,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	cmd := fsm.Command{
		Type:    fsm.RegisterWorker,
		Payload: payloadBytes,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	if err := s.raft.Propose(cmdBytes); err != nil {
		return nil, err
	}

	return &pb.RegisterWorkerResponse{
		WorkerId: workerID,
		Success:  true,
	}, nil
}

// GetWorker handles a request to get a worker.
func (s *Server) GetWorker(ctx context.Context, req *pb.GetWorkerRequest) (*pb.GetWorkerResponse, error) {
	// For now, we don't have sharding, so we just return a random worker.
	// In the future, this should use consistent hashing on the vector_id.

	// This is a read operation, so we can query the FSM directly.
	// The leader will have the most up-to-date data. Followers might be slightly behind.
	// To ensure read-after-write consistency, clients should talk to the leader.

	s.fsm.Mu.Lock()
	defer s.fsm.Mu.Unlock()

	workers := s.fsm.GetWorkers()
	if len(workers) == 0 {
		return nil, fmt.Errorf("no workers available")
	}

	// Simple round-robin for now
	worker := workers[s.fsm.WorkerIdx%len(workers)]
	s.fsm.WorkerIdx++

	return &pb.GetWorkerResponse{
		Address: worker.Address,
	}, nil
}

// ListWorkers returns a list of all registered workers.
func (s *Server) ListWorkers(ctx context.Context, req *pb.ListWorkersRequest) (*pb.ListWorkersResponse, error) {
	s.fsm.Mu.RLock()
	defer s.fsm.Mu.RUnlock()

	workers := s.fsm.GetWorkers()

	workerInfos := make([]*pb.WorkerInfo, 0, len(workers))
	for _, w := range workers {
		workerInfos = append(workerInfos, &pb.WorkerInfo{
			WorkerId:      w.ID,
			Address:       w.Address,
			LastHeartbeat: w.LastHeartbeat.Unix(),
		})
	}

	return &pb.ListWorkersResponse{
		Workers: workerInfos,
	}, nil
}
