// This file implements the gRPC server for the Placement Driver service.
// It handles RPCs for worker registration, heartbeats, service discovery,
// and collection management. It interacts with the Raft layer to ensure
// that all state changes are consistent and fault-tolerant.

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"time"

	"github.com/pavandhadge/vectron/placementdriver/internal/fsm"
	"github.com/pavandhadge/vectron/placementdriver/internal/raft"
	pb "github.com/pavandhadge/vectron/shared/proto/placementdriver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// raftTimeout is the default timeout for Raft proposals.
	raftTimeout = 5 * time.Second
	// defaultInitialShards is the number of shards created for a new collection by default.
	defaultInitialShards = 8
)

// Server implements the gRPC PlacementService.
type Server struct {
	pb.UnimplementedPlacementServiceServer
	raft *raft.Node // The underlying Raft node for proposing state changes.
	fsm  *fsm.FSM   // A direct reference to the FSM for read-only operations.
}

// NewServer creates a new instance of the placement driver gRPC server.
func NewServer(r *raft.Node, f *fsm.FSM) *Server {
	return &Server{
		raft: r,
		fsm:  f,
	}
}

// RegisterWorker handles a worker's request to join the cluster.
// It proposes a `RegisterWorker` command to the Raft log.
func (s *Server) RegisterWorker(ctx context.Context, req *pb.RegisterWorkerRequest) (*pb.RegisterWorkerResponse, error) {
	if req.GetGrpcAddress() == "" {
		return nil, status.Error(codes.InvalidArgument, "worker grpc_address is required")
	}
	if req.GetRaftAddress() == "" {
		return nil, status.Error(codes.InvalidArgument, "worker raft_address is required")
	}

	// Create the payload for the FSM command.
	payload := fsm.RegisterWorkerPayload{
		GrpcAddress: req.GetGrpcAddress(),
		RaftAddress: req.GetRaftAddress(),
		CPUCores:    req.GetCpuCores(),
		MemoryBytes: req.GetMemoryBytes(),
		DiskBytes:   req.GetDiskBytes(),
		Rack:        req.GetRack(),
		Zone:        req.GetZone(),
		Region:      req.GetRegion(),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal payload: %v", err)
	}

	// Create and marshal the command.
	cmd := fsm.Command{Type: fsm.RegisterWorker, Payload: payloadBytes}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal command: %v", err)
	}

	// Propose the command to the Raft cluster.
	res, err := s.raft.Propose(cmdBytes, raftTimeout)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose command: %v", err)
	}

	newWorkerID := res.Value
	if newWorkerID == 0 {
		return nil, status.Errorf(codes.Internal, "FSM failed to assign a worker ID")
	}

	return &pb.RegisterWorkerResponse{
		WorkerId: strconv.FormatUint(newWorkerID, 10),
		Success:  true,
	}, nil
}

// ShardAssignment contains all the information a worker needs to manage a shard replica.
// This is a DTO sent to workers in the HeartbeatResponse.
type ShardAssignment struct {
	ShardInfo      *fsm.ShardInfo    `json:"shard_info"`
	InitialMembers map[uint64]string `json:"initial_members"` // map[nodeID]raftAddress
}

// Heartbeat is called periodically by workers to signal they are alive.
// It also serves as the mechanism for the placement driver to send shard assignments to workers.
func (s *Server) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	workerID, err := strconv.ParseUint(req.WorkerId, 10, 64)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid worker_id format: %v", err)
	}

	// Propose a command to update the worker's last heartbeat time and load metrics in the FSM.
	hbPayload := fsm.UpdateWorkerHeartbeatPayload{
		WorkerID:           workerID,
		CPUUsagePercent:    float64(req.GetCpuUsagePercent()),
		MemoryUsagePercent: float64(req.GetMemoryUsagePercent()),
		DiskUsagePercent:   float64(req.GetDiskUsagePercent()),
		QueriesPerSecond:   float64(req.GetQueriesPerSecond()),
		ActiveShards:       req.GetActiveShards(),
		VectorCount:        req.GetVectorCount(),
		MemoryBytes:        req.GetMemoryBytes(),
	}
	payloadBytes, err := json.Marshal(hbPayload)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal heartbeat payload: %v", err)
	}
	cmd := fsm.Command{Type: fsm.UpdateWorkerHeartbeat, Payload: payloadBytes}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal heartbeat command: %v", err)
	}

	// Propose the heartbeat update. We log a warning on failure but don't fail the request,
	// as the primary goal is to return shard assignments.
	if _, err := s.raft.Propose(cmdBytes, raftTimeout); err != nil {
		fmt.Printf("Warning: failed to propose heartbeat for worker %d: %v\n", workerID, err)
	}

	// Propose commands to update shard metrics for hot-shard detection
	for _, shardMetric := range req.GetShardMetrics() {
		metricsPayload := fsm.ShardMetricsPayload{
			WorkerID:         workerID,
			ShardID:          shardMetric.GetShardId(),
			QueriesPerSecond: float64(shardMetric.GetQueriesPerSecond()),
			VectorCount:      shardMetric.GetVectorCount(),
			AvgLatencyMs:     float64(shardMetric.GetAvgLatencyMs()),
		}
		metricsPayloadBytes, err := json.Marshal(metricsPayload)
		if err != nil {
			fmt.Printf("Warning: failed to marshal shard metrics payload: %v\n", err)
			continue
		}
		metricsCmd := fsm.Command{Type: fsm.UpdateShardMetrics, Payload: metricsPayloadBytes}
		metricsCmdBytes, err := json.Marshal(metricsCmd)
		if err != nil {
			fmt.Printf("Warning: failed to marshal shard metrics command: %v\n", err)
			continue
		}
		if _, err := s.raft.Propose(metricsCmdBytes, raftTimeout); err != nil {
			fmt.Printf("Warning: failed to propose shard metrics for shard %d: %v\n", shardMetric.GetShardId(), err)
		}
	}

	// Propose commands to update shard leader information.
	for _, leaderInfo := range req.ShardLeaderInfo {
		leaderPayload := fsm.UpdateShardLeaderPayload{
			ShardID:  leaderInfo.ShardId,
			LeaderID: leaderInfo.LeaderId,
		}
		leaderPayloadBytes, err := json.Marshal(leaderPayload)
		if err != nil {
			fmt.Printf("Warning: failed to marshal leader payload: %v\n", err)
			continue
		}
		leaderCmd := fsm.Command{Type: fsm.UpdateShardLeader, Payload: leaderPayloadBytes}
		leaderCmdBytes, err := json.Marshal(leaderCmd)
		if err != nil {
			fmt.Printf("Warning: failed to marshal leader command: %v\n", err)
			continue
		}
		if _, err := s.raft.Propose(leaderCmdBytes, raftTimeout); err != nil {
			fmt.Printf("Warning: failed to propose leader update for shard %d: %v\n", leaderInfo.ShardId, err)
		}
	}

	// Read the current state from the FSM to find all shards assigned to this worker.
	assignments := make([]*ShardAssignment, 0)
	allWorkers := s.fsm.GetWorkers()
	workerMap := make(map[uint64]fsm.WorkerInfo, len(allWorkers))
	for _, w := range allWorkers {
		workerMap[w.ID] = w
	}

	for _, coll := range s.fsm.GetCollections() {
		for _, shard := range coll.Shards {
			isReplica := false
			for _, replicaID := range shard.Replicas {
				if replicaID == workerID {
					isReplica = true
					break
				}
			}

			// If the worker is a replica for this shard, build the assignment details.
			if isReplica {
				initialMembers := make(map[uint64]string)
				for _, replicaID := range shard.Replicas {
					if worker, ok := workerMap[replicaID]; ok {
						initialMembers[replicaID] = worker.RaftAddress
					} else {
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

	// Serialize the shard assignments and send them back to the worker.
	assignmentBytes, err := json.Marshal(assignments)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to serialize shard assignments: %v", err)
	}

	return &pb.HeartbeatResponse{
		Ok:      true,
		Message: string(assignmentBytes),
	}, nil
}

// GetWorker handles a request from the API gateway to find the correct worker for an operation.
// It uses consistent hashing on the vector ID to determine the shard.
func (s *Server) GetWorker(ctx context.Context, req *pb.GetWorkerRequest) (*pb.GetWorkerResponse, error) {
	if req.Collection == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name is required")
	}

	// Read from the local FSM state. This is a read-only operation.
	collection, ok := s.fsm.GetCollection(req.Collection)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "collection '%s' not found", req.Collection)
	}
	if len(collection.Shards) == 0 {
		return nil, status.Errorf(codes.Internal, "collection '%s' has no shards", req.Collection)
	}

	var targetShard *fsm.ShardInfo

	// If a vector ID is provided, use hashing to find the correct shard.
	if req.VectorId != "" {
		hash := fnv.New64a()
		hash.Write([]byte(req.VectorId))
		hashValue := hash.Sum64()

		for _, shard := range collection.Shards {
			if hashValue >= shard.KeyRangeStart && hashValue <= shard.KeyRangeEnd {
				targetShard = shard
				break
			}
		}
	} else {
		// If no vector ID is provided (e.g., for a collection-wide search),
		// we can return any shard. Here, we pick the first one for simplicity.
		keys := make([]uint64, 0, len(collection.Shards))
		for k := range collection.Shards {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		targetShard = collection.Shards[keys[0]]
	}

	if targetShard == nil {
		return nil, status.Errorf(codes.NotFound, "no suitable shard found for vector_id '%s'", req.VectorId)
	}
	if len(targetShard.Replicas) == 0 {
		return nil, status.Errorf(codes.Internal, "shard '%d' has no replicas", targetShard.ShardID)
	}

	// TODO: Return the address of the LEADER of the shard's Raft group.
	// For now, we return the address of the first replica in the list.
	firstReplicaID := targetShard.Replicas[0]
	worker, ok := s.fsm.GetWorker(firstReplicaID)
	if !ok {
		return nil, status.Errorf(codes.Internal, "worker with ID '%d' not found for shard '%d'", firstReplicaID, targetShard.ShardID)
	}

	return &pb.GetWorkerResponse{
		GrpcAddress: worker.GrpcAddress,
		ShardId:     uint32(targetShard.ShardID),
	}, nil
}

// ListWorkers returns a list of all registered workers from the FSM state.
func (s *Server) ListWorkers(ctx context.Context, req *pb.ListWorkersRequest) (*pb.ListWorkersResponse, error) {
	workers := s.fsm.GetWorkers()

	workerInfos := make([]*pb.WorkerInfo, 0, len(workers))
	for _, w := range workers {
		// Derive collections hosted on this worker.
		collectionSet := make(map[string]struct{})
		for _, shard := range s.fsm.GetShardsOnWorker(w.ID) {
			collectionSet[shard.Collection] = struct{}{}
		}
		collections := make([]string, 0, len(collectionSet))
		for name := range collectionSet {
			collections = append(collections, name)
		}

		workerInfos = append(workerInfos, &pb.WorkerInfo{
			WorkerId:      strconv.FormatUint(w.ID, 10),
			GrpcAddress:   w.GrpcAddress,
			RaftAddress:   w.RaftAddress,
			Collections:   collections,
			LastHeartbeat: w.LastHeartbeat.Unix(),
			Healthy:       s.fsm.IsWorkerHealthy(w.ID),
			Metadata:      map[string]string{},
		})
	}

	return &pb.ListWorkersResponse{
		Workers: workerInfos,
	}, nil
}

func (s *Server) ListWorkersForCollection(ctx context.Context, req *pb.ListWorkersForCollectionRequest) (*pb.ListWorkersForCollectionResponse, error) {
	if req.Collection == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name is required")
	}

	collection, ok := s.fsm.GetCollection(req.Collection)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "collection '%s' not found", req.Collection)
	}

	// Use a map to store unique gRPC addresses
	uniqueGrpcAddresses := make(map[string]struct{})

	for _, shard := range collection.Shards {
		for _, replicaID := range shard.Replicas {
			worker, ok := s.fsm.GetWorker(replicaID)
			if !ok {
				// This should ideally not happen if FSM state is consistent
				fmt.Printf("Warning: worker with ID '%d' not found for shard '%d'\n", replicaID, shard.ShardID)
				continue
			}
			uniqueGrpcAddresses[worker.GrpcAddress] = struct{}{}
		}
	}

	grpcAddresses := make([]string, 0, len(uniqueGrpcAddresses))
	for addr := range uniqueGrpcAddresses {
		grpcAddresses = append(grpcAddresses, addr)
	}

	return &pb.ListWorkersForCollectionResponse{
		GrpcAddresses: grpcAddresses,
	}, nil
}

// CreateCollection handles the RPC to create a new collection.
// It proposes a `CreateCollection` command to the Raft log.
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

	cmd := fsm.Command{Type: fsm.CreateCollection, Payload: payloadBytes}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal command: %v", err)
	}

	res, err := s.raft.Propose(cmdBytes, raftTimeout)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose command: %v", err)
	}

	if res.Value == 0 {
		// This could happen if the collection already exists or there are not enough workers.
		// The specific error is logged by the FSM.
		return nil, status.Errorf(codes.Internal, "FSM failed to create collection")
	}

	return &pb.CreateCollectionResponse{Success: true}, nil
}

// ListCollections returns a list of all collection names from the FSM state.
func (s *Server) ListCollections(ctx context.Context, req *pb.ListCollectionsRequest) (*pb.ListCollectionsResponse, error) {
	collections := s.fsm.GetCollections()
	collectionNames := make([]string, 0, len(collections))
	for _, c := range collections {
		collectionNames = append(collectionNames, c.Name)
	}
	return &pb.ListCollectionsResponse{Collections: collectionNames}, nil
}

// GetCollectionStatus handles the RPC to get the status of a collection.
func (s *Server) GetCollectionStatus(ctx context.Context, req *pb.GetCollectionStatusRequest) (*pb.GetCollectionStatusResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name is required")
	}

	collection, ok := s.fsm.GetCollection(req.Name)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "collection '%s' not found", req.Name)
	}

	shardStatuses := make([]*pb.ShardStatus, 0, len(collection.Shards))
	for _, shard := range collection.Shards {
		shardStatuses = append(shardStatuses, &pb.ShardStatus{
			ShardId:  uint32(shard.ShardID),
			Replicas: shard.Replicas,
			LeaderId: shard.LeaderID,
			Ready:    shard.LeaderID > 0,
		})
	}

	return &pb.GetCollectionStatusResponse{
		Name:      collection.Name,
		Dimension: collection.Dimension,
		Distance:  collection.Distance,
		Shards:    shardStatuses,
	}, nil
}
