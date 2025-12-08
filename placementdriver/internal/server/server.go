package server

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/google/uuid"
	"github.com/pavandhadge/vectron/placementdriver/internal/fsm"
	"github.com/pavandhadge/vectron/placementdriver/internal/raft"
	pb "github.com/pavandhadge/vectron/placementdriver/proto/placementdriver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the PlacementService gRPC service.
type Server struct {
	pb.UnimplementedPlacementServiceServer
	raft *raft.Raft
	fsm  *fsm.FSM

	mu        sync.Mutex
	workerIdx int
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
		return nil, status.Errorf(codes.Internal, "failed to marshal payload: %v", err)
	}

	cmd := fsm.Command{
		Type:    fsm.RegisterWorker,
		Payload: payloadBytes,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal command: %v", err)
	}

	if err := s.raft.Propose(cmdBytes); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose command: %v", err)
	}

	return &pb.RegisterWorkerResponse{
		WorkerId: workerID,
		Success:  true,
	}, nil
}

// GetWorker handles a request to get a worker for a specific collection.
func (s *Server) GetWorker(ctx context.Context, req *pb.GetWorkerRequest) (*pb.GetWorkerResponse, error) {
	if req.Collection == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name is required")
	}

	// This is a read operation, so we can query the FSM directly.
	// For read-after-write consistency, clients should ideally talk to the leader.
	collection, ok := s.fsm.GetCollection(req.Collection)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "collection '%s' not found", req.Collection)
	}

	worker, ok := s.fsm.GetWorker(collection.WorkerID)
	if !ok {
		// This indicates an inconsistent state, which should ideally not happen.
		return nil, status.Errorf(codes.Internal, "worker with ID '%s' not found for collection '%s'", collection.WorkerID, req.Collection)
	}

	return &pb.GetWorkerResponse{
		Address: worker.Address,
	}, nil
}

// ListWorkers returns a list of all registered workers.
func (s *Server) ListWorkers(ctx context.Context, req *pb.ListWorkersRequest) (*pb.ListWorkersResponse, error) {
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

// CreateCollection creates a new collection and assigns it to a worker.
func (s *Server) CreateCollection(ctx context.Context, req *pb.CreateCollectionRequest) (*pb.CreateCollectionResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name is required")
	}

	workers := s.fsm.GetWorkers()
	if len(workers) == 0 {
		return nil, status.Error(codes.FailedPrecondition, "no workers available to assign collection to")
	}

	// Assign a worker using round-robin.
	s.mu.Lock()
	worker := workers[s.workerIdx%len(workers)]
	s.workerIdx++
	s.mu.Unlock()

	payload := fsm.CreateCollectionPayload{
		Name:      req.Name,
		Dimension: req.Dimension,
		Distance:  req.Distance,
		WorkerID:  worker.ID,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal payload: %v", err)
	}

	cmd := fsm.Command{
		Type:    fsm.CreateCollection,
		Payload: payloadBytes,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal command: %v", err)
	}

	if err := s.raft.Propose(cmdBytes); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose command: %v", err)
	}

	return &pb.CreateCollectionResponse{Success: true}, nil
}

// ListCollections returns a list of all collections.
func (s *Server) ListCollections(ctx context.Context, req *pb.ListCollectionsRequest) (*pb.ListCollectionsResponse, error) {
	collections := s.fsm.GetCollections()
	collectionNames := make([]string, 0, len(collections))
	for _, c := range collections {
		collectionNames = append(collectionNames, c.Name)
	}
	return &pb.ListCollectionsResponse{Collections: collectionNames}, nil
}
