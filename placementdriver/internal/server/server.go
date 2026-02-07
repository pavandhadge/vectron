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
	"strings"
	"sync"
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
	defaultInitialShards = 16
	// shardLeaseTTL is the routing lease duration returned to gateways.
	shardLeaseTTL = 20 * time.Second
)

// Server implements the gRPC PlacementService.
type Server struct {
	pb.UnimplementedPlacementServiceServer
	raft *raft.Node // The underlying Raft node for proposing state changes.
	fsm  *fsm.FSM   // A direct reference to the FSM for read-only operations.

	membershipMu   sync.RWMutex
	shardMembership map[uint64]shardMembershipSnapshot

	progressMu     sync.RWMutex
	shardProgress  map[uint64]map[uint64]shardProgressSnapshot

	leaderAckMu    sync.RWMutex
	leaderTransferAcks map[uint64]map[uint64]leaderTransferAck
}

// NewServer creates a new instance of the placement driver gRPC server.
func NewServer(r *raft.Node, f *fsm.FSM) *Server {
	return &Server{
		raft: r,
		fsm:  f,
		shardMembership: make(map[uint64]shardMembershipSnapshot),
		shardProgress: make(map[uint64]map[uint64]shardProgressSnapshot),
		leaderTransferAcks: make(map[uint64]map[uint64]leaderTransferAck),
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

	role := "write"
	for _, cap := range req.GetCapabilities() {
		if strings.EqualFold(cap, "search_only") {
			role = "search_only"
			break
		}
	}

	// Create the payload for the FSM command.
	payload := fsm.RegisterWorkerPayload{
		GrpcAddress: req.GetGrpcAddress(),
		RaftAddress: req.GetRaftAddress(),
		Role:        role,
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
	Bootstrap      bool              `json:"bootstrap"`       // Whether this replica should bootstrap the shard.
}

type shardMembershipSnapshot struct {
	nodeIDs        map[uint64]struct{}
	configChangeID uint64
	leaderWorkerID uint64
	updatedAt      time.Time
}

const membershipStaleAfter = 30 * time.Second

type shardProgressSnapshot struct {
	appliedIndex uint64
	shardEpoch   uint64
	updatedAt    time.Time
}

const progressStaleAfter = 30 * time.Second

type leaderTransferAck struct {
	toNodeID  uint64
	shardEpoch uint64
	updatedAt time.Time
}

const leaderAckStaleAfter = 5 * time.Minute

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
		RunningShards:      req.GetRunningShards(),
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

	leaderMap := make(map[uint64]uint64, len(req.ShardLeaderInfo))

	// Propose commands to update shard leader information.
	for _, leaderInfo := range req.ShardLeaderInfo {
		if !s.fsm.IsWorkerAssignedToShard(workerID, leaderInfo.ShardId) {
			continue
		}
		leaderMap[leaderInfo.ShardId] = leaderInfo.LeaderId
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

	// Update shard membership info (leader-reported).
	s.updateShardMembership(workerID, req.GetShardMembership(), leaderMap)
	s.updateShardProgress(workerID, req.GetShardProgress())
	s.updateLeaderTransferAcks(workerID, req.GetLeaderTransferAcks())

	// Read the current state from the FSM to find all shards assigned to this worker.
	assignments := make([]*ShardAssignment, 0)
	allWorkers := s.fsm.GetWorkers()
	workerMap := make(map[uint64]fsm.WorkerInfo, len(allWorkers))
	workerRole := ""
	for _, w := range allWorkers {
		workerMap[w.ID] = w
		if w.ID == workerID {
			workerRole = w.Role
		}
	}

	for _, coll := range s.fsm.GetCollections() {
		for _, shard := range coll.Shards {
			isReplica := false
			if workerRole != "search_only" {
				for _, replicaID := range shard.Replicas {
					if replicaID == workerID {
						isReplica = true
						break
					}
				}
			} else {
				isReplica = true
			}

			// If the worker is a replica for this shard, build the assignment details.
			if isReplica {
				initialMembers := make(map[uint64]string)
				if workerRole != "search_only" {
					for _, replicaID := range shard.Replicas {
						if worker, ok := workerMap[replicaID]; ok {
							initialMembers[replicaID] = worker.RaftAddress
						} else {
							fmt.Printf("Warning: could not find worker address for replica %d in shard %d\n", replicaID, shard.ShardID)
						}
					}
				}

				bootstrap := false
				if workerRole != "search_only" {
					if !shard.Bootstrapped && len(shard.BootstrapMembers) > 0 {
						for _, memberID := range shard.BootstrapMembers {
							if memberID == workerID {
								bootstrap = true
								break
							}
						}
					}
				}

				assignments = append(assignments, &ShardAssignment{
					ShardInfo:      shard,
					InitialMembers: initialMembers,
					Bootstrap:      bootstrap,
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
		Ok:               true,
		Message:          string(assignmentBytes),
		AssignmentsEpoch: s.fsm.GetAssignmentsEpoch(),
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

	// Prefer the shard leader if known and healthy; otherwise fall back to any healthy replica.
	var targetWorker *fsm.WorkerInfo
	if targetShard.LeaderID != 0 {
		if w, ok := s.fsm.GetWorker(targetShard.LeaderID); ok && s.fsm.IsWorkerHealthy(w.ID) {
			workerCopy := w
			targetWorker = &workerCopy
		}
	}

	if targetWorker == nil {
		for _, replicaID := range targetShard.Replicas {
			if w, ok := s.fsm.GetWorker(replicaID); ok && s.fsm.IsWorkerHealthy(w.ID) {
				workerCopy := w
				targetWorker = &workerCopy
				break
			}
		}
	}

	if targetWorker == nil {
		return nil, status.Errorf(codes.Unavailable, "no healthy replicas available for shard '%d'", targetShard.ShardID)
	}

	return &pb.GetWorkerResponse{
		GrpcAddress: targetWorker.GrpcAddress,
		ShardId:     uint32(targetShard.ShardID),
		ShardEpoch:  targetShard.Epoch,
		LeaseExpiryUnixMs: time.Now().Add(shardLeaseTTL).UnixMilli(),
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

		state := pb.WorkerState_WORKER_STATE_UNKNOWN
		switch w.State {
		case fsm.WorkerStateJoining:
			state = pb.WorkerState_WORKER_STATE_JOINING
		case fsm.WorkerStateReady:
			state = pb.WorkerState_WORKER_STATE_READY
		case fsm.WorkerStateDraining:
			state = pb.WorkerState_WORKER_STATE_DRAINING
		}

		role := w.Role
		if role == "" {
			role = "write"
		}
		metadata := map[string]string{
			"role": role,
		}
		workerInfos = append(workerInfos, &pb.WorkerInfo{
			WorkerId:      strconv.FormatUint(w.ID, 10),
			GrpcAddress:   w.GrpcAddress,
			RaftAddress:   w.RaftAddress,
			Collections:   collections,
			LastHeartbeat: w.LastHeartbeat.Unix(),
			Healthy:       s.fsm.IsWorkerHealthy(w.ID),
			Metadata:      metadata,
			State:         state,
		})
	}

	return &pb.ListWorkersResponse{
		Workers: workerInfos,
	}, nil
}

// DrainWorker marks a worker as draining (no new assignments, move shards away).
func (s *Server) DrainWorker(ctx context.Context, req *pb.DrainWorkerRequest) (*pb.DrainWorkerResponse, error) {
	workerID, err := strconv.ParseUint(req.GetWorkerId(), 10, 64)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid worker_id format: %v", err)
	}

	payload := fsm.UpdateWorkerStatePayload{
		WorkerID: workerID,
		State:    fsm.WorkerStateDraining,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal drain worker payload: %v", err)
	}
	cmd := fsm.Command{Type: fsm.UpdateWorkerState, Payload: payloadBytes}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal drain worker command: %v", err)
	}
	if _, err := s.raft.Propose(cmdBytes, raftTimeout); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose drain worker: %v", err)
	}

	return &pb.DrainWorkerResponse{Success: true}, nil
}

// RemoveWorker removes a worker from the cluster state (must have no shards).
func (s *Server) RemoveWorker(ctx context.Context, req *pb.RemoveWorkerRequest) (*pb.RemoveWorkerResponse, error) {
	workerID, err := strconv.ParseUint(req.GetWorkerId(), 10, 64)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid worker_id format: %v", err)
	}

	payload := fsm.RemoveWorkerPayload{WorkerID: workerID}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal remove worker payload: %v", err)
	}
	cmd := fsm.Command{Type: fsm.RemoveWorker, Payload: payloadBytes}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal remove worker command: %v", err)
	}
	if _, err := s.raft.Propose(cmdBytes, raftTimeout); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose remove worker: %v", err)
	}

	return &pb.RemoveWorkerResponse{Success: true}, nil
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
	// Include healthy search-only workers for read routing if they report shard readiness.
	shardToCollection := make(map[uint64]string)
	for _, coll := range s.fsm.GetCollections() {
		for _, shard := range coll.Shards {
			shardToCollection[shard.ShardID] = coll.Name
		}
	}
	for _, worker := range s.fsm.GetWorkers() {
		if worker.Role != "search_only" || !s.fsm.IsWorkerHealthy(worker.ID) {
			continue
		}
		hasCollection := false
		for _, shardID := range worker.RunningShards {
			if shardToCollection[shardID] == req.Collection {
				hasCollection = true
				break
			}
		}
		if !hasCollection {
			continue
		}
		uniqueGrpcAddresses[worker.GrpcAddress] = struct{}{}
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

	workerMap := make(map[uint64]fsm.WorkerInfo)
	searchOnlyAddrs := make(map[uint64][]string)
	shardToCollection := make(map[uint64]string)
	for _, coll := range s.fsm.GetCollections() {
		for _, shard := range coll.Shards {
			shardToCollection[shard.ShardID] = coll.Name
		}
	}
	for _, w := range s.fsm.GetWorkers() {
		workerMap[w.ID] = w
		if w.Role == "search_only" && s.fsm.IsWorkerHealthy(w.ID) {
			for _, shardID := range w.RunningShards {
				if shardToCollection[shardID] == collection.Name {
					searchOnlyAddrs[shardID] = append(searchOnlyAddrs[shardID], w.GrpcAddress)
				}
			}
		}
	}

	shardStatuses := make([]*pb.ShardStatus, 0, len(collection.Shards))
	for _, shard := range collection.Shards {
		var leaderAddr string
		if shard.LeaderID != 0 {
			if w, ok := workerMap[shard.LeaderID]; ok && s.fsm.IsWorkerHealthy(w.ID) {
				leaderAddr = w.GrpcAddress
			}
		}
		replicaAddrs := make([]string, 0, len(shard.Replicas))
		replicaSet := make(map[string]struct{})
		for _, replicaID := range shard.Replicas {
			if w, ok := workerMap[replicaID]; ok && s.fsm.IsWorkerHealthy(w.ID) {
				if _, ok := replicaSet[w.GrpcAddress]; !ok {
					replicaSet[w.GrpcAddress] = struct{}{}
					replicaAddrs = append(replicaAddrs, w.GrpcAddress)
				}
			}
		}
		if addrs, ok := searchOnlyAddrs[shard.ShardID]; ok {
			for _, addr := range addrs {
				if _, ok := replicaSet[addr]; ok {
					continue
				}
				replicaSet[addr] = struct{}{}
				replicaAddrs = append(replicaAddrs, addr)
			}
		}

		shardStatuses = append(shardStatuses, &pb.ShardStatus{
			ShardId:              uint32(shard.ShardID),
			Replicas:             shard.Replicas,
			LeaderId:             shard.LeaderID,
			Ready:                shard.LeaderID > 0,
			KeyRangeStart:        shard.KeyRangeStart,
			KeyRangeEnd:          shard.KeyRangeEnd,
			ShardEpoch:           shard.Epoch,
			LeaderGrpcAddress:    leaderAddr,
			ReplicaGrpcAddresses: replicaAddrs,
		})
	}

	return &pb.GetCollectionStatusResponse{
		Name:      collection.Name,
		Dimension: collection.Dimension,
		Distance:  collection.Distance,
		Shards:    shardStatuses,
	}, nil
}

func (s *Server) updateShardMembership(workerID uint64, infos []*pb.ShardMembershipInfo, leaderMap map[uint64]uint64) {
	if len(infos) == 0 {
		return
	}

	s.membershipMu.Lock()
	defer s.membershipMu.Unlock()

	now := time.Now()
	for _, info := range infos {
		if info == nil {
			continue
		}
		leaderID, ok := leaderMap[info.ShardId]
		if !ok || leaderID != workerID {
			continue
		}
		if !s.fsm.IsWorkerAssignedToShard(workerID, info.ShardId) {
			continue
		}
		nodeIDs := make(map[uint64]struct{}, len(info.NodeIds))
		for _, nodeID := range info.NodeIds {
			nodeIDs[nodeID] = struct{}{}
		}
		s.shardMembership[info.ShardId] = shardMembershipSnapshot{
			nodeIDs:        nodeIDs,
			configChangeID: info.ConfigChangeId,
			leaderWorkerID: workerID,
			updatedAt:      now,
		}
	}
}

func (s *Server) isShardMember(shardID uint64, nodeID uint64) (bool, bool) {
	s.membershipMu.RLock()
	defer s.membershipMu.RUnlock()

	snap, ok := s.shardMembership[shardID]
	if !ok {
		return false, false
	}
	if time.Since(snap.updatedAt) > membershipStaleAfter {
		return false, false
	}
	_, exists := snap.nodeIDs[nodeID]
	return exists, true
}

func (s *Server) getShardMembershipSnapshot(shardID uint64) (shardMembershipSnapshot, bool) {
	s.membershipMu.RLock()
	defer s.membershipMu.RUnlock()

	snap, ok := s.shardMembership[shardID]
	if !ok {
		return shardMembershipSnapshot{}, false
	}
	if time.Since(snap.updatedAt) > membershipStaleAfter {
		return shardMembershipSnapshot{}, false
	}
	nodeCopy := make(map[uint64]struct{}, len(snap.nodeIDs))
	for id := range snap.nodeIDs {
		nodeCopy[id] = struct{}{}
	}
	return shardMembershipSnapshot{
		nodeIDs:        nodeCopy,
		configChangeID: snap.configChangeID,
		leaderWorkerID: snap.leaderWorkerID,
		updatedAt:      snap.updatedAt,
	}, true
}

func (s *Server) updateShardProgress(workerID uint64, infos []*pb.ShardProgressInfo) {
	if len(infos) == 0 {
		return
	}

	s.progressMu.Lock()
	defer s.progressMu.Unlock()

	now := time.Now()
	for _, info := range infos {
		if info == nil {
			continue
		}
		if !s.fsm.IsWorkerAssignedToShard(workerID, info.ShardId) {
			continue
		}
		shardMap := s.shardProgress[info.ShardId]
		if shardMap == nil {
			shardMap = make(map[uint64]shardProgressSnapshot)
			s.shardProgress[info.ShardId] = shardMap
		}
		shardMap[workerID] = shardProgressSnapshot{
			appliedIndex: info.AppliedIndex,
			shardEpoch:   info.ShardEpoch,
			updatedAt:    now,
		}
	}
}

func (s *Server) getShardProgress(shardID uint64, nodeID uint64) (shardProgressSnapshot, bool) {
	s.progressMu.RLock()
	defer s.progressMu.RUnlock()

	nodes, ok := s.shardProgress[shardID]
	if !ok {
		return shardProgressSnapshot{}, false
	}
	snap, ok := nodes[nodeID]
	if !ok {
		return shardProgressSnapshot{}, false
	}
	if time.Since(snap.updatedAt) > progressStaleAfter {
		return shardProgressSnapshot{}, false
	}
	return snap, true
}

func (s *Server) updateLeaderTransferAcks(workerID uint64, infos []*pb.ShardLeaderTransferAck) {
	if len(infos) == 0 {
		return
	}

	s.leaderAckMu.Lock()
	defer s.leaderAckMu.Unlock()

	now := time.Now()
	for _, ack := range infos {
		if ack == nil {
			continue
		}
		if ack.FromNodeId != workerID {
			continue
		}
		shardMap := s.leaderTransferAcks[ack.ShardId]
		if shardMap == nil {
			shardMap = make(map[uint64]leaderTransferAck)
			s.leaderTransferAcks[ack.ShardId] = shardMap
		}
		shardMap[ack.FromNodeId] = leaderTransferAck{
			toNodeID:  ack.ToNodeId,
			shardEpoch: ack.ShardEpoch,
			updatedAt: now,
		}
	}
}

func (s *Server) getLeaderTransferAck(shardID uint64, fromNodeID uint64) (leaderTransferAck, bool) {
	s.leaderAckMu.RLock()
	defer s.leaderAckMu.RUnlock()

	nodes, ok := s.leaderTransferAcks[shardID]
	if !ok {
		return leaderTransferAck{}, false
	}
	ack, ok := nodes[fromNodeID]
	if !ok {
		return leaderTransferAck{}, false
	}
	if time.Since(ack.updatedAt) > leaderAckStaleAfter {
		return leaderTransferAck{}, false
	}
	return ack, true
}
