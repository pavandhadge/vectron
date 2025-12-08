package server

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	"github.com/pavandhadge/vectron/placementdriver/internal/fsm"
	"github.com/pavandhadge/vectron/placementdriver/internal/raft"
	pb "github.com/pavandhadge/vectron/placementdriver/proto/placementdriver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	raftTimeout          = 5 * time.Second
	defaultInitialShards = 8
)

// Server implements the PlacementService gRPC service.
type Server struct {
	pb.UnimplementedPlacementServiceServer
	raft *raft.Node
	fsm  *fsm.FSM

	mu        sync.Mutex
	workerIdx int
}

// NewServer creates a new Server.
func NewServer(r *raft.Node, f *fsm.FSM) *Server {
	return &Server{
		raft: r,
		fsm:  f,
	}
}

// RegisterWorker handles a worker registration request. The FSM assigns a new unique uint64 ID.
func (s *Server) RegisterWorker(ctx context.Context, req *pb.RegisterWorkerRequest) (*pb.RegisterWorkerResponse, error) {
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "worker address is required")
	}

	payload := fsm.RegisterWorkerPayload{
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

	res, err := s.raft.Propose(cmdBytes, raftTimeout)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose command: %v", err)
	}

	newWorkerID := res.Value
	if newWorkerID == 0 {
		return nil, status.Errorf(codes.Internal, "FSM failed to assign worker ID")
	}

	return &pb.RegisterWorkerResponse{
		WorkerId: strconv.FormatUint(newWorkerID, 10),
		Success:  true,
	}, nil
}

// ShardAssignment contains all info a worker needs to manage a shard replica.
type ShardAssignment struct {
	ShardInfo      *fsm.ShardInfo    `json:"shard_info"`
	InitialMembers map[uint64]string `json:"initial_members"` // map[nodeID]raftAddress
}

// Heartbeat is called by a worker to signal it's alive and to get its shard assignments.
func (s *Server) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	workerID, err := strconv.ParseUint(req.WorkerId, 10, 64)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid worker_id format: %v", err)
	}

	// Propose a command to update the worker's last heartbeat time.
	hbPayload := fsm.UpdateWorkerHeartbeatPayload{WorkerID: workerID}
	payloadBytes, err := json.Marshal(hbPayload)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal heartbeat payload: %v", err)
	}
	cmd := fsm.Command{Type: fsm.UpdateWorkerHeartbeat, Payload: payloadBytes}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal heartbeat command: %v", err)
	}

	if _, err := s.raft.Propose(cmdBytes, raftTimeout); err != nil {
		fmt.Printf("Warning: failed to propose heartbeat for worker %d: %v\n", workerID, err)
	}

	// Find all shards assigned to this worker and enrich with peer addresses.
	assignments := make([]*ShardAssignment, 0)
	allWorkers := s.fsm.GetWorkers()
	workerMap := make(map[uint64]fsm.WorkerInfo, len(allWorkers))
	for _, w := range allWorkers {
		workerMap[w.ID] = w
	}

	collections := s.fsm.GetCollections()
	for _, coll := range collections {
		for _, shard := range coll.Shards {
			isReplica := false
			for _, replicaID := range shard.Replicas {
				if replicaID == workerID {
					isReplica = true
					break
				}
			}

			if isReplica {
				initialMembers := make(map[uint64]string)
				for _, replicaID := range shard.Replicas {
					if worker, ok := workerMap[replicaID]; ok {
						initialMembers[replicaID] = worker.Address
					} else {
						// This replica's worker is not registered or known. This is an inconsistent state.
						fmt.Printf("Warning: could not find worker address for replica %d in shard %d\n", replicaID, shard.ShardID)
					}
				}

				assignments = append(assignments, &ShardAssignment{
					ShardInfo:      shard,
					InitialMembers: initialMembers,
				})
			}
		}
	}

	// Serialize the shard list and send it back to the worker.
	assignmentBytes, err := json.Marshal(assignments)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to serialize shard assignments: %v", err)
	}

	return &pb.HeartbeatResponse{
		Ok:      true,
		Message: string(assignmentBytes),
	}, nil
}

// GetWorker handles a request to get a worker for a specific collection and vector ID.
func (s *Server) GetWorker(ctx context.Context, req *pb.GetWorkerRequest) (*pb.GetWorkerResponse, error) {
	if req.Collection == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name is required")
	}

	collection, ok := s.fsm.GetCollection(req.Collection)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "collection '%s' not found", req.Collection)
	}

	if len(collection.Shards) == 0 {
		return nil, status.Errorf(codes.Internal, "collection '%s' has no shards", req.Collection)
	}

	var targetShard *fsm.ShardInfo
	if req.VectorId == "" {
		// If no vector ID is provided, return the first shard for now.
		// In a real scenario, this might route to a default shard or load balance.
		for _, shard := range collection.Shards {
			targetShard = shard
			break
		}
	} else {
		hash := fnv.New64a()
		hash.Write([]byte(req.VectorId))
		hashValue := hash.Sum64()
		for _, shard := range collection.Shards {
			if hashValue >= shard.KeyRangeStart && hashValue <= shard.KeyRangeEnd {
				targetShard = shard
				break
			}
		}
	}

	if targetShard == nil {
		return nil, status.Errorf(codes.NotFound, "no suitable shard found for vector_id '%s'", req.VectorId)
	}

	if len(targetShard.Replicas) == 0 {
		return nil, status.Errorf(codes.Internal, "shard '%d' has no replicas", targetShard.ShardID)
	}

	// TODO: Return the address of the LEADER of the replica group.
	// For now, we return the address of the first replica.
	firstReplicaID := targetShard.Replicas[0]
	worker, ok := s.fsm.GetWorker(firstReplicaID)
	if !ok {
		return nil, status.Errorf(codes.Internal, "worker with ID '%d' not found for shard '%d'", firstReplicaID, targetShard.ShardID)
	}

	return &pb.GetWorkerResponse{
		Address: worker.Address,
		ShardId: uint32(targetShard.ShardID),
	}, nil
}

// ListWorkers returns a list of all registered workers.
func (s *Server) ListWorkers(ctx context.Context, req *pb.ListWorkersRequest) (*pb.ListWorkersResponse, error) {
	workers := s.fsm.GetWorkers()

	workerInfos := make([]*pb.WorkerInfo, 0, len(workers))
	for _, w := range workers {
		workerInfos = append(workerInfos, &pb.WorkerInfo{
			WorkerId:      strconv.FormatUint(w.ID, 10),
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

	payload := fsm.CreateCollectionPayload{
		Name:          req.Name,
		Dimension:     req.Dimension,
		Distance:      req.Distance,
		InitialShards: defaultInitialShards,
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

	if _, err := s.raft.Propose(cmdBytes, raftTimeout); err != nil {
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
