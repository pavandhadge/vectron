// This file defines the Finite State Machine (FSM) for the placement driver.
// The FSM is the core of the placement driver's state management. It is a deterministic
// state machine that processes commands from the Raft log and updates the cluster state.
// The state includes information about peers, workers, collections, and shards.

package fsm

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	sm "github.com/lni/dragonboat/v3/statemachine"
)

// CommandType defines the type of operation to be applied to the FSM.
type CommandType int

const (
	// RegisterPeer adds a new peer to the placement driver's Raft cluster.
	RegisterPeer CommandType = iota
	// RegisterWorker adds a new worker node to the cluster.
	RegisterWorker
	// CreateCollection creates a new collection and its initial shards.
	CreateCollection
	// UpdateWorkerHeartbeat updates the last heartbeat timestamp for a worker.
	UpdateWorkerHeartbeat
	// UpdateWorkerState updates a worker's lifecycle state.
	UpdateWorkerState
	// RemoveWorker removes a worker from the cluster state.
	RemoveWorker
	// UpdateShardLeader updates the leader of a shard.
	UpdateShardLeader
	// MoveShard initiates a shard migration from one worker to another.
	MoveShard
	// UpdateShardMetrics updates per-shard metrics for hot-shard detection.
	UpdateShardMetrics
	// AddShardReplica adds a replica to a shard's desired replica set.
	AddShardReplica
	// RemoveShardReplica removes a replica from a shard's desired replica set.
	RemoveShardReplica
)

// Command is the structure that is serialized and sent to the Raft log.
// It contains the type of command and its JSON-encoded payload.
type Command struct {
	Type    CommandType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// RegisterPeerPayload is the data for the RegisterPeer command.
type RegisterPeerPayload struct {
	ID       string `json:"id"`
	RaftAddr string `json:"raft_addr"`
	APIAddr  string `json:"api_addr"`
}

// RegisterWorkerPayload is the data for the RegisterWorker command.
type RegisterWorkerPayload struct {
	GrpcAddress string `json:"grpc_address"`
	RaftAddress string `json:"raft_address"`
	// Capacity information for capacity-weighted placement
	CPUCores    int32 `json:"cpu_cores"`
	MemoryBytes int64 `json:"memory_bytes"`
	DiskBytes   int64 `json:"disk_bytes"`
	// Failure domain information for fault-tolerant placement
	Rack   string `json:"rack"`
	Zone   string `json:"zone"`
	Region string `json:"region"`
}

// UpdateWorkerStatePayload is the data for the UpdateWorkerState command.
type UpdateWorkerStatePayload struct {
	WorkerID uint64      `json:"worker_id"`
	State    WorkerState `json:"state"`
}

// RemoveWorkerPayload is the data for the RemoveWorker command.
type RemoveWorkerPayload struct {
	WorkerID uint64 `json:"worker_id"`
}

// CreateCollectionPayload is the data for the CreateCollection command.
type CreateCollectionPayload struct {
	Name          string `json:"name"`
	Dimension     int32  `json:"dimension"`
	Distance      string `json:"distance"`
	InitialShards int    `json:"initial_shards"`
}

// UpdateWorkerHeartbeatPayload is the data for the UpdateWorkerHeartbeat command.
type UpdateWorkerHeartbeatPayload struct {
	WorkerID           uint64  `json:"worker_id"`
	CPUUsagePercent    float64 `json:"cpu_usage_percent"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`
	DiskUsagePercent   float64 `json:"disk_usage_percent"`
	QueriesPerSecond   float64 `json:"queries_per_second"`
	ActiveShards       int64   `json:"active_shards"`
	VectorCount        int64   `json:"vector_count"`
	MemoryBytes        int64   `json:"memory_bytes"`
	RunningShards      []uint64 `json:"running_shards"`
}

// UpdateShardLeaderPayload is the data for the UpdateShardLeader command.
type UpdateShardLeaderPayload struct {
	ShardID  uint64 `json:"shard_id"`
	LeaderID uint64 `json:"leader_id"`
}

// MoveShardPayload is the data for the MoveShard command.
type MoveShardPayload struct {
	ShardID        uint64 `json:"shard_id"`
	SourceWorkerID uint64 `json:"source_worker_id"`
	TargetWorkerID uint64 `json:"target_worker_id"`
}

// AddShardReplicaPayload adds a replica to a shard.
type AddShardReplicaPayload struct {
	ShardID   uint64 `json:"shard_id"`
	ReplicaID uint64 `json:"replica_id"`
}

// RemoveShardReplicaPayload removes a replica from a shard.
type RemoveShardReplicaPayload struct {
	ShardID   uint64 `json:"shard_id"`
	ReplicaID uint64 `json:"replica_id"`
}

// ShardMetricsPayload contains per-shard metrics for hot-shard detection
type ShardMetricsPayload struct {
	WorkerID         uint64  `json:"worker_id"`
	ShardID          uint64  `json:"shard_id"`
	QueriesPerSecond float64 `json:"queries_per_second"`
	VectorCount      int64   `json:"vector_count"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
}

// ======================================================================================
// State Machine Data Structures
// ======================================================================================

// ShardInfo holds the metadata for a single shard, including its key range and replicas.
type ShardInfo struct {
	ShardID       uint64   `json:"shard_id"`
	Collection    string   `json:"collection"`
	KeyRangeStart uint64   `json:"key_range_start"`
	KeyRangeEnd   uint64   `json:"key_range_end"`
	Replicas      []uint64 `json:"replicas"`  // Slice of worker node IDs that host this shard.
	LeaderID      uint64   `json:"leader_id"` // The current leader of the shard's Raft group.
	Dimension     int32    `json:"dimension"`
	Distance      string   `json:"distance"`
	Bootstrapped  bool     `json:"bootstrapped"` // Whether the raft group has been bootstrapped.
	BootstrapMembers []uint64 `json:"bootstrap_members"` // Initial members allowed to bootstrap.
	// Metrics for hot-shard detection
	QueriesPerSecond float64   `json:"queries_per_second"` // Current query rate
	TotalQueries     int64     `json:"total_queries"`      // Cumulative query count
	AvgLatencyMs     float64   `json:"avg_latency_ms"`     // Average latency
	LastUpdated      time.Time `json:"last_updated"`       // Last metrics update time
}

// Collection holds the metadata for a single collection, including its shards.
type Collection struct {
	Name      string                `json:"name"`
	Dimension int32                 `json:"dimension"`
	Distance  string                `json:"distance"`
	Shards    map[uint64]*ShardInfo `json:"shards"` // map[shardID]*ShardInfo
}

// PeerInfo holds information about a peer in the placement driver's Raft cluster.
type PeerInfo struct {
	ID       string `json:"id"`
	RaftAddr string `json:"raft_addr"`
	APIAddr  string `json:"api_addr"`
}

// WorkerState represents the lifecycle state of a worker.
type WorkerState int

const (
	WorkerStateUnknown WorkerState = iota
	WorkerStateJoining
	WorkerStateReady
	WorkerStateDraining
)

// WorkerInfo holds information about a registered worker node.
type WorkerInfo struct {
	ID            uint64    `json:"id"`
	GrpcAddress   string    `json:"grpc_address"`
	RaftAddress   string    `json:"raft_address"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	State         WorkerState `json:"state"`
	RunningShards []uint64  `json:"running_shards"`
	// Load metrics for load-aware placement
	CPUUsagePercent    float64 `json:"cpu_usage_percent"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`
	DiskUsagePercent   float64 `json:"disk_usage_percent"`
	QueriesPerSecond   float64 `json:"queries_per_second"`
	ActiveShards       int64   `json:"active_shards"`
	VectorCount        int64   `json:"vector_count"`
	MemoryBytes        int64   `json:"memory_bytes"`
	// Capacity information for capacity-weighted placement
	CPUCores      int32 `json:"cpu_cores"`      // Total CPU cores
	TotalMemory   int64 `json:"total_memory"`   // Total memory in bytes
	TotalDisk     int64 `json:"total_disk"`     // Total disk in bytes
	TotalCapacity int64 `json:"total_capacity"` // Normalized capacity score
	// Failure domain information for fault-tolerant placement
	Rack   string `json:"rack"`   // Rack identifier
	Zone   string `json:"zone"`   // Availability zone
	Region string `json:"region"` // Region
}

// ======================================================================================
// FSM Implementation
// ======================================================================================

// FSM is the placement driver's state machine. It implements the
// dragonboat.IOnDiskStateMachine interface.
type FSM struct {
	mu sync.RWMutex
	// Peers stores information about the other PD nodes in the Raft cluster.
	Peers map[string]PeerInfo `json:"peers"`
	// Workers stores information about all registered worker nodes.
	Workers map[uint64]WorkerInfo `json:"workers"`
	// Collections stores all the collections and their shard information.
	Collections map[string]*Collection `json:"collections"`
	// AssignmentsEpoch is a monotonic epoch for shard assignments.
	AssignmentsEpoch uint64 `json:"assignments_epoch"`
	// NextShardID is a counter for generating unique shard IDs.
	NextShardID uint64 `json:"next_shard_id"`
	// NextWorkerID is a counter for generating unique worker IDs.
	NextWorkerID uint64 `json:"next_worker_id"`
}

// NewFSM creates a new, empty FSM.
func NewFSM() *FSM {
	return &FSM{
		Peers:        make(map[string]PeerInfo),
		Workers:      make(map[uint64]WorkerInfo),
		Collections:  make(map[string]*Collection),
		AssignmentsEpoch: 1,
		NextShardID:  1,
		NextWorkerID: 1,
	}
}

// Open is called by Dragonboat when the FSM is started.
func (f *FSM) Open(stopc <-chan struct{}) (uint64, error) {
	// No-op for this implementation. We recover state from snapshots.
	return 0, nil
}

// Update is the core method of the FSM. It is called by Dragonboat to apply
// committed log entries to the state machine.
func (f *FSM) Update(entries []sm.Entry) ([]sm.Entry, error) {
	for i, entry := range entries {
		var cmd Command
		if err := json.Unmarshal(entry.Cmd, &cmd); err != nil {
			return nil, fmt.Errorf("failed to unmarshal command: %w", err)
		}

		var appErr error
		var result uint64
		// Dispatch the command to the appropriate apply method.
		switch cmd.Type {
		case RegisterPeer:
			var payload RegisterPeerPayload
			if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
				appErr = fmt.Errorf("failed to unmarshal RegisterPeer payload: %w", err)
			} else {
				f.applyRegisterPeer(payload)
			}
		case RegisterWorker:
			var payload RegisterWorkerPayload
			if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
				appErr = fmt.Errorf("failed to unmarshal RegisterWorker payload: %w", err)
			} else {
				result = f.applyRegisterWorker(payload)
			}
		case CreateCollection:
			var payload CreateCollectionPayload
			if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
				appErr = fmt.Errorf("failed to unmarshal CreateCollection payload: %w", err)
			} else {
				if err := f.applyCreateCollection(payload); err != nil {
					appErr = err
					result = 0 // Explicitly set 0 on failure.
				} else {
					result = 1 // Set 1 on success.
				}
			}
		case UpdateWorkerHeartbeat:
			var payload UpdateWorkerHeartbeatPayload
			if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
				appErr = fmt.Errorf("failed to unmarshal UpdateWorkerHeartbeat payload: %w", err)
			} else {
				f.applyUpdateWorkerHeartbeat(payload)
			}
		case UpdateWorkerState:
			var payload UpdateWorkerStatePayload
			if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
				appErr = fmt.Errorf("failed to unmarshal UpdateWorkerState payload: %w", err)
			} else {
				if err := f.applyUpdateWorkerState(payload); err != nil {
					appErr = err
				}
			}
		case RemoveWorker:
			var payload RemoveWorkerPayload
			if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
				appErr = fmt.Errorf("failed to unmarshal RemoveWorker payload: %w", err)
			} else {
				if err := f.applyRemoveWorker(payload); err != nil {
					appErr = err
				}
			}
		case UpdateShardLeader:
			var payload UpdateShardLeaderPayload
			if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
				appErr = fmt.Errorf("failed to unmarshal UpdateShardLeader payload: %w", err)
			} else {
				f.applyUpdateShardLeader(payload)
			}
		case MoveShard:
			var payload MoveShardPayload
			if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
				appErr = fmt.Errorf("failed to unmarshal MoveShard payload: %w", err)
			} else {
				if err := f.applyMoveShard(payload); err != nil {
					appErr = err
					result = 0
				} else {
					result = 1
				}
			}
		case UpdateShardMetrics:
			var payload ShardMetricsPayload
			if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
				appErr = fmt.Errorf("failed to unmarshal ShardMetrics payload: %w", err)
			} else {
				f.applyUpdateShardMetrics(payload)
			}
		case AddShardReplica:
			var payload AddShardReplicaPayload
			if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
				appErr = fmt.Errorf("failed to unmarshal AddShardReplica payload: %w", err)
			} else {
				if err := f.applyAddShardReplica(payload); err != nil {
					appErr = err
				}
			}
		case RemoveShardReplica:
			var payload RemoveShardReplicaPayload
			if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
				appErr = fmt.Errorf("failed to unmarshal RemoveShardReplica payload: %w", err)
			} else {
				if err := f.applyRemoveShardReplica(payload); err != nil {
					appErr = err
				}
			}
		default:
			appErr = fmt.Errorf("unknown command type: %d", cmd.Type)
		}

		if appErr != nil {
			fmt.Printf("Error applying command: %v\n", appErr)
		}
		// The result of the update is passed back to the proposer.
		entries[i].Result = sm.Result{Value: result}
	}
	return entries, nil
}

// Sync is called by Dragonboat to synchronize the FSM with the latest state.
// In this implementation, all state is in memory, so this is a no-op.
func (f *FSM) Sync() error {
	return nil
}

func (f *FSM) applyRegisterPeer(payload RegisterPeerPayload) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Peers[payload.ID] = PeerInfo{
		ID:       payload.ID,
		RaftAddr: payload.RaftAddr,
		APIAddr:  payload.APIAddr,
	}
}

func (f *FSM) applyRegisterWorker(payload RegisterWorkerPayload) uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Reuse existing worker ID if the addresses match to avoid ID drift.
	for id, info := range f.Workers {
		if info.GrpcAddress == payload.GrpcAddress && info.RaftAddress == payload.RaftAddress {
			info.LastHeartbeat = time.Now().UTC()
			f.Workers[id] = info
			fmt.Printf("Worker already registered as %d (GRPC %s, Raft %s)\n", id, info.GrpcAddress, info.RaftAddress)
			return id
		}
	}

	workerID := f.NextWorkerID
	f.NextWorkerID++

	// Calculate normalized capacity score
	// Formula: weighted sum of normalized resources
	// CPU cores: weight 0.3, Memory: weight 0.4, Disk: weight 0.3
	// Base normalization: 8 cores, 32GB RAM, 500GB disk = 1000 base capacity
	cpuScore := float64(payload.CPUCores) / 8.0 * 300.0
	memScore := float64(payload.MemoryBytes) / (32 * 1024 * 1024 * 1024) * 400.0
	diskScore := float64(payload.DiskBytes) / (500 * 1024 * 1024 * 1024) * 300.0
	totalCapacity := int64(cpuScore + memScore + diskScore)
	if totalCapacity < 100 {
		totalCapacity = 100 // Minimum capacity to avoid division issues
	}

	f.Workers[workerID] = WorkerInfo{
		ID:            workerID,
		GrpcAddress:   payload.GrpcAddress,
		RaftAddress:   payload.RaftAddress,
		LastHeartbeat: time.Now().UTC(),
		State:         WorkerStateJoining,
		CPUCores:      payload.CPUCores,
		TotalMemory:   payload.MemoryBytes,
		TotalDisk:     payload.DiskBytes,
		TotalCapacity: totalCapacity,
		Rack:          payload.Rack,
		Zone:          payload.Zone,
		Region:        payload.Region,
	}
	fmt.Printf("Registered new worker %d with GRPC address %s, Raft address %s, capacity=%d, rack=%s, zone=%s, region=%s\n",
		workerID, payload.GrpcAddress, payload.RaftAddress, totalCapacity, payload.Rack, payload.Zone, payload.Region)
	return workerID
}

func (f *FSM) applyCreateCollection(payload CreateCollectionPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	eligibleWorkers := 0
	for _, worker := range f.Workers {
		if f.isWorkerEligibleForPlacement(worker) {
			eligibleWorkers++
		}
	}
	fmt.Printf("Attempting to create collection '%s'. Workers available: %d\n", payload.Name, eligibleWorkers)

	if _, ok := f.Collections[payload.Name]; ok {
		return fmt.Errorf("collection %s already exists", payload.Name)
	}

	const replicationFactor = DefaultReplicationFactor // TODO: Make this configurable per collection.
	if eligibleWorkers == 0 {
		err := fmt.Errorf("no eligible workers available for collection creation")
		fmt.Printf("Error creating collection: %v\n", err)
		return err
	}
	actualReplication := replicationFactor
	if eligibleWorkers < replicationFactor {
		actualReplication = eligibleWorkers
		fmt.Printf("Warning: creating collection '%s' with %d replicas (target %d). Under-replication will be repaired when more workers join.\n",
			payload.Name, actualReplication, replicationFactor)
	}

	collection := &Collection{
		Name:      payload.Name,
		Dimension: payload.Dimension,
		Distance:  payload.Distance,
		Shards:    make(map[uint64]*ShardInfo),
	}

	// Create initial shards for the collection.
	numShards := payload.InitialShards
	if numShards <= 0 {
		numShards = 1 // Default to at least one shard.
	}
	shardRangeSize := uint64(math.MaxUint64 / float64(numShards))

	// Get workers sorted by load (least loaded first)
	workerIDs := f.getWorkersSortedByLoad()

	// Assign replicas to shards using failure domain-aware placement.
	// This ensures replicas are spread across different racks/zones for fault tolerance.
	for i := 0; i < numShards; i++ {
		shardID := f.NextShardID
		f.NextShardID++

		startKey := uint64(i) * shardRangeSize
		endKey := (uint64(i+1) * shardRangeSize) - 1
		if i == numShards-1 {
			endKey = math.MaxUint64 // Ensure the last shard covers the rest of the key space.
		}

		// Select replicas using failure domain-aware placement
		replicas := f.selectReplicasWithFailureDomain(workerIDs, actualReplication)

		shard := &ShardInfo{
			ShardID:       shardID,
			Collection:    payload.Name,
			KeyRangeStart: startKey,
			KeyRangeEnd:   endKey,
			Replicas:      replicas,
			BootstrapMembers: append([]uint64(nil), replicas...),
			Dimension:     payload.Dimension,
			Distance:      payload.Distance,
		}
		collection.Shards[shardID] = shard
	}

	f.Collections[collection.Name] = collection
	f.bumpAssignmentsEpochLocked()
	fmt.Printf("FSM: Successfully created collection '%s' with %d shards\n", collection.Name, numShards)
	return nil
}

// getWorkersSortedByLoad returns worker IDs sorted by capacity-normalized load (least loaded first)
// Uses capacity-weighted placement: bigger nodes get proportionally more shards
func (f *FSM) getWorkersSortedByLoad() []uint64 {
	type workerLoad struct {
		id        uint64
		loadRatio float64 // load score / capacity (lower is better)
		rawLoad   float64
		capacity  int64
	}

	// Calculate total capacity of all workers for proportional allocation
	var totalCapacity int64
	for _, worker := range f.Workers {
		if f.isWorkerEligibleForPlacement(worker) {
			totalCapacity += worker.TotalCapacity
		}
	}

	loads := make([]workerLoad, 0, len(f.Workers))
	for id, worker := range f.Workers {
		if !f.isWorkerEligibleForPlacement(worker) {
			continue
		}
		rawLoad := calculateLoadScore(worker)
		// Load ratio = raw load / normalized capacity
		// This allows bigger nodes to have higher absolute load before being considered "full"
		capacity := float64(worker.TotalCapacity)
		if capacity < 100 {
			capacity = 100 // Minimum capacity
		}

		// Calculate expected load based on capacity proportion
		// If a node has 2x capacity, it can handle 2x load before reaching same load ratio
		loadRatio := rawLoad / capacity

		loads = append(loads, workerLoad{
			id:        id,
			loadRatio: loadRatio,
			rawLoad:   rawLoad,
			capacity:  worker.TotalCapacity,
		})
	}

	// Sort by load ratio (ascending - nodes with most spare capacity relative to their size first)
	for i := 0; i < len(loads); i++ {
		for j := i + 1; j < len(loads); j++ {
			if loads[j].loadRatio < loads[i].loadRatio {
				loads[i], loads[j] = loads[j], loads[i]
			}
		}
	}

	result := make([]uint64, len(loads))
	for i, wl := range loads {
		result[i] = wl.id
	}
	return result
}

// getWorkersByCapacity returns workers sorted by total capacity (largest first)
// Useful for initial placement when we want to fill big nodes first
func (f *FSM) getWorkersByCapacity() []uint64 {
	type workerCap struct {
		id       uint64
		capacity int64
	}

	caps := make([]workerCap, 0, len(f.Workers))
	for id, worker := range f.Workers {
		caps = append(caps, workerCap{id: id, capacity: worker.TotalCapacity})
	}

	// Sort by capacity (descending - largest nodes first)
	for i := 0; i < len(caps); i++ {
		for j := i + 1; j < len(caps); j++ {
			if caps[j].capacity > caps[i].capacity {
				caps[i], caps[j] = caps[j], caps[i]
			}
		}
	}

	result := make([]uint64, len(caps))
	for i, wc := range caps {
		result[i] = wc.id
	}
	return result
}

// calculateLoadScore computes a composite load score (lower is better)
// This is the raw load score before capacity normalization
func calculateLoadScore(worker WorkerInfo) float64 {
	// Weight factors for different metrics
	const (
		cpuWeight    = 0.3
		memoryWeight = 0.3
		diskWeight   = 0.15
		queryWeight  = 0.15
		shardWeight  = 0.1
	)

	// Normalize query rate (assume 1000 QPS is high load)
	normalizedQueries := math.Min(worker.QueriesPerSecond/1000.0, 1.0)

	// Normalize active shards (assume 100 shards is high load)
	normalizedShards := math.Min(float64(worker.ActiveShards)/100.0, 1.0)

	score := worker.CPUUsagePercent*cpuWeight +
		worker.MemoryUsagePercent*memoryWeight +
		worker.DiskUsagePercent*diskWeight +
		normalizedQueries*queryWeight*100 +
		normalizedShards*shardWeight*100

	return score
}

func (f *FSM) isWorkerEligibleForPlacement(worker WorkerInfo) bool {
	if worker.State != WorkerStateReady {
		return false
	}
	return time.Since(worker.LastHeartbeat) < WorkerTimeout
}

// selectReplicasWithFailureDomain selects replica workers ensuring they are spread across failure domains
// Priority: Zone > Rack (tries to spread across zones first, then racks within zones)
func (f *FSM) selectReplicasWithFailureDomain(sortedWorkerIDs []uint64, replicationFactor int) []uint64 {
	if replicationFactor <= 1 {
		// Single replica, just return the first (least loaded) worker
		if len(sortedWorkerIDs) > 0 {
			return []uint64{sortedWorkerIDs[0]}
		}
		return nil
	}

	selected := make([]uint64, 0, replicationFactor)
	usedZones := make(map[string]bool)
	usedRacks := make(map[string]bool)

	// First pass: try to select workers from different zones
	for _, workerID := range sortedWorkerIDs {
		if len(selected) >= replicationFactor {
			break
		}

		worker, ok := f.Workers[workerID]
		if !ok {
			continue
		}

		// Check if this zone is already used
		zoneKey := worker.Region + "/" + worker.Zone
		if !usedZones[zoneKey] {
			selected = append(selected, workerID)
			usedZones[zoneKey] = true
			usedRacks[worker.Region+"/"+worker.Zone+"/"+worker.Rack] = true
		}
	}

	// Second pass: if we still need more replicas, try different racks within used zones
	if len(selected) < replicationFactor {
		for _, workerID := range sortedWorkerIDs {
			if len(selected) >= replicationFactor {
				break
			}

			// Skip already selected workers
			alreadySelected := false
			for _, s := range selected {
				if s == workerID {
					alreadySelected = true
					break
				}
			}
			if alreadySelected {
				continue
			}

			worker, ok := f.Workers[workerID]
			if !ok {
				continue
			}

			// Check if this rack is already used
			rackKey := worker.Region + "/" + worker.Zone + "/" + worker.Rack
			if !usedRacks[rackKey] {
				selected = append(selected, workerID)
				usedRacks[rackKey] = true
			}
		}
	}

	// Third pass: if still need more, just fill with remaining workers (best effort)
	if len(selected) < replicationFactor {
		for _, workerID := range sortedWorkerIDs {
			if len(selected) >= replicationFactor {
				break
			}

			// Skip already selected workers
			alreadySelected := false
			for _, s := range selected {
				if s == workerID {
					alreadySelected = true
					break
				}
			}
			if !alreadySelected {
				selected = append(selected, workerID)
			}
		}
	}

	return selected
}

const loadMetricDecayFactor = 0.7 // Exponential decay factor for smoothing load metrics

func (f *FSM) applyUpdateWorkerHeartbeat(payload UpdateWorkerHeartbeatPayload) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if worker, ok := f.Workers[payload.WorkerID]; ok {
		worker.LastHeartbeat = time.Now().UTC()
		if worker.State == WorkerStateJoining || worker.State == WorkerStateUnknown {
			worker.State = WorkerStateReady
		}

		// Apply exponential decay to smooth load metrics
		worker.CPUUsagePercent = worker.CPUUsagePercent*loadMetricDecayFactor + payload.CPUUsagePercent*(1-loadMetricDecayFactor)
		worker.MemoryUsagePercent = worker.MemoryUsagePercent*loadMetricDecayFactor + payload.MemoryUsagePercent*(1-loadMetricDecayFactor)
		worker.DiskUsagePercent = worker.DiskUsagePercent*loadMetricDecayFactor + payload.DiskUsagePercent*(1-loadMetricDecayFactor)
		worker.QueriesPerSecond = worker.QueriesPerSecond*loadMetricDecayFactor + payload.QueriesPerSecond*(1-loadMetricDecayFactor)

		// Direct values (not decayed)
		worker.ActiveShards = payload.ActiveShards
		worker.VectorCount = payload.VectorCount
		worker.MemoryBytes = payload.MemoryBytes
		worker.RunningShards = append([]uint64(nil), payload.RunningShards...)

		f.Workers[payload.WorkerID] = worker
	}
}

func (f *FSM) applyUpdateWorkerState(payload UpdateWorkerStatePayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	worker, ok := f.Workers[payload.WorkerID]
	if !ok {
		return fmt.Errorf("worker %d not found", payload.WorkerID)
	}
	worker.State = payload.State
	f.Workers[payload.WorkerID] = worker
	return nil
}

func (f *FSM) applyRemoveWorker(payload RemoveWorkerPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.Workers[payload.WorkerID]; !ok {
		return fmt.Errorf("worker %d not found", payload.WorkerID)
	}
	for _, collection := range f.Collections {
		for _, shard := range collection.Shards {
			for _, replicaID := range shard.Replicas {
				if replicaID == payload.WorkerID {
					return fmt.Errorf("worker %d still has shard %d", payload.WorkerID, shard.ShardID)
				}
			}
		}
	}
	delete(f.Workers, payload.WorkerID)
	return nil
}

func (f *FSM) applyUpdateShardLeader(payload UpdateShardLeaderPayload) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, collection := range f.Collections {
		if shard, ok := collection.Shards[payload.ShardID]; ok {
			shard.LeaderID = payload.LeaderID
			if payload.LeaderID > 0 {
				shard.Bootstrapped = true
				shard.BootstrapMembers = nil
			}
			return
		}
	}
}

func (f *FSM) applyUpdateShardMetrics(payload ShardMetricsPayload) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, collection := range f.Collections {
		if shard, ok := collection.Shards[payload.ShardID]; ok {
			// Apply exponential decay to smooth metrics
			const decayFactor = 0.7
			shard.QueriesPerSecond = shard.QueriesPerSecond*decayFactor + payload.QueriesPerSecond*(1-decayFactor)
			shard.AvgLatencyMs = shard.AvgLatencyMs*decayFactor + payload.AvgLatencyMs*(1-decayFactor)
			shard.TotalQueries += int64(payload.QueriesPerSecond) // Approximate
			shard.LastUpdated = time.Now().UTC()
			return
		}
	}
}

func (f *FSM) applyAddShardReplica(payload AddShardReplicaPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var targetShard *ShardInfo
	for _, collection := range f.Collections {
		if shard, ok := collection.Shards[payload.ShardID]; ok {
			targetShard = shard
			break
		}
	}

	if targetShard == nil {
		return fmt.Errorf("shard %d not found", payload.ShardID)
	}

	worker, ok := f.Workers[payload.ReplicaID]
	if !ok {
		return fmt.Errorf("worker %d not found", payload.ReplicaID)
	}
	if worker.State != WorkerStateReady {
		return fmt.Errorf("worker %d is not ready", payload.ReplicaID)
	}
	if time.Since(worker.LastHeartbeat) >= WorkerTimeout {
		return fmt.Errorf("worker %d is unhealthy", payload.ReplicaID)
	}

	for _, replicaID := range targetShard.Replicas {
		if replicaID == payload.ReplicaID {
			return nil // idempotent
		}
	}

	if len(targetShard.Replicas) >= MaxReplicationFactor {
		return fmt.Errorf("shard %d already has max replicas (%d)", payload.ShardID, MaxReplicationFactor)
	}

	targetShard.Replicas = append(targetShard.Replicas, payload.ReplicaID)
	f.bumpAssignmentsEpochLocked()
	return nil
}

func (f *FSM) applyRemoveShardReplica(payload RemoveShardReplicaPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var targetShard *ShardInfo
	for _, collection := range f.Collections {
		if shard, ok := collection.Shards[payload.ShardID]; ok {
			targetShard = shard
			break
		}
	}

	if targetShard == nil {
		return fmt.Errorf("shard %d not found", payload.ShardID)
	}

	index := -1
	for i, replicaID := range targetShard.Replicas {
		if replicaID == payload.ReplicaID {
			index = i
			break
		}
	}
	if index == -1 {
		return fmt.Errorf("replica %d not found in shard %d", payload.ReplicaID, payload.ShardID)
	}

	minAllowed := DefaultReplicationFactor
	if len(targetShard.Replicas) < minAllowed {
		minAllowed = len(targetShard.Replicas)
	}
	if len(targetShard.Replicas)-1 < minAllowed {
		return fmt.Errorf("cannot remove replica %d from shard %d (min required replicas: %d)",
			payload.ReplicaID, payload.ShardID, minAllowed)
	}

	targetShard.Replicas = append(targetShard.Replicas[:index], targetShard.Replicas[index+1:]...)
	f.bumpAssignmentsEpochLocked()
	return nil
}

func (f *FSM) applyMoveShard(payload MoveShardPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Find the shard
	var targetShard *ShardInfo
	var targetCollection string
	for collName, collection := range f.Collections {
		if shard, ok := collection.Shards[payload.ShardID]; ok {
			targetShard = shard
			targetCollection = collName
			break
		}
	}

	if targetShard == nil {
		return fmt.Errorf("shard %d not found", payload.ShardID)
	}

	// Verify source worker has this shard
	hasShard := false
	for _, replicaID := range targetShard.Replicas {
		if replicaID == payload.SourceWorkerID {
			hasShard = true
			break
		}
	}
	if !hasShard {
		return fmt.Errorf("source worker %d does not have shard %d", payload.SourceWorkerID, payload.ShardID)
	}

	// Verify target worker exists
	if _, ok := f.Workers[payload.TargetWorkerID]; !ok {
		return fmt.Errorf("target worker %d not found", payload.TargetWorkerID)
	}

	// Replace source with target in replicas list
	for i, replicaID := range targetShard.Replicas {
		if replicaID == payload.SourceWorkerID {
			targetShard.Replicas[i] = payload.TargetWorkerID
			break
		}
	}

	f.bumpAssignmentsEpochLocked()
	fmt.Printf("FSM: Moved shard %d from worker %d to worker %d (collection: %s)\n",
		payload.ShardID, payload.SourceWorkerID, payload.TargetWorkerID, targetCollection)
	return nil
}

func (f *FSM) bumpAssignmentsEpochLocked() {
	f.AssignmentsEpoch++
	if f.AssignmentsEpoch == 0 {
		f.AssignmentsEpoch = 1
	}
}

// Lookup is used for read-only queries of the FSM's state. It is called by
// Dragonboat for linearizable reads.
func (f *FSM) Lookup(query interface{}) (interface{}, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// The query parameter is not used in this simple implementation.
	// A more advanced implementation could use it to specify which part of the state to return.
	// For now, we return a snapshot of the entire FSM state.
	return &fsmSnapshot{
		Peers:        f.Peers,
		Workers:      f.Workers,
		Collections:  f.Collections,
		AssignmentsEpoch: f.AssignmentsEpoch,
		NextShardID:  f.NextShardID,
		NextWorkerID: f.NextWorkerID,
	}, nil
}

// fsmSnapshot is a struct used for serializing the FSM state for snapshotting.
type fsmSnapshot struct {
	Peers        map[string]PeerInfo    `json:"peers"`
	Workers      map[uint64]WorkerInfo  `json:"workers"`
	Collections  map[string]*Collection `json:"collections"`
	AssignmentsEpoch uint64             `json:"assignments_epoch"`
	NextShardID  uint64                 `json:"next_shard_id"`
	NextWorkerID uint64                 `json:"next_worker_id"`
}

// PrepareSnapshot is called by Dragonboat to get a snapshot of the current state.
// In this implementation, we do all the work in SaveSnapshot, so this is a no-op.
func (f *FSM) PrepareSnapshot() (interface{}, error) {
	return nil, nil
}

// SaveSnapshot is called by Dragonboat to create a snapshot of the FSM's state.
// It serializes the FSM's data to a writer.
func (f *FSM) SaveSnapshot(ctx interface{}, w io.Writer, done <-chan struct{}) error {
	f.mu.RLock()
	defer f.mu.RUnlock()

	data := fsmSnapshot{
		Peers:        f.Peers,
		Workers:      f.Workers,
		Collections:  f.Collections,
		AssignmentsEpoch: f.AssignmentsEpoch,
		NextShardID:  f.NextShardID,
		NextWorkerID: f.NextWorkerID,
	}

	return json.NewEncoder(w).Encode(data)
}

// RecoverFromSnapshot is called by Dragonboat to restore the FSM's state from a snapshot.
// It deserializes the data from a reader and applies it to the FSM.
func (f *FSM) RecoverFromSnapshot(r io.Reader, done <-chan struct{}) error {
	var data fsmSnapshot
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.Peers = data.Peers
	f.Workers = data.Workers
	f.Collections = data.Collections
	f.AssignmentsEpoch = data.AssignmentsEpoch
	f.NextShardID = data.NextShardID
	f.NextWorkerID = data.NextWorkerID
	if f.AssignmentsEpoch == 0 {
		f.AssignmentsEpoch = 1
	}

	return nil
}

// Close is called by Dragonboat when the FSM is being closed.
func (f *FSM) Close() error { return nil }

// GetHash is used by Dragonboat to check the consistency of the FSM.
func (f *FSM) GetHash() (uint64, error) { return 0, nil }

// ======================================================================================
// Helper methods for safely accessing FSM state (used by the gRPC server).
// ======================================================================================

// GetPeer returns information about a specific peer.
func (f *FSM) GetPeer(id string) (PeerInfo, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	peer, ok := f.Peers[id]
	return peer, ok
}

// GetWorker returns information about a specific worker.
func (f *FSM) GetWorker(id uint64) (WorkerInfo, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	worker, ok := f.Workers[id]
	return worker, ok
}

// GetAssignmentsEpoch returns the current assignments epoch.
func (f *FSM) GetAssignmentsEpoch() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.AssignmentsEpoch
}

// GetWorkers returns a slice of all registered workers.
func (f *FSM) GetWorkers() []WorkerInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()
	workers := make([]WorkerInfo, 0, len(f.Workers))
	for _, w := range f.Workers {
		workers = append(workers, w)
	}
	return workers
}

// GetCollection returns information about a specific collection.
func (f *FSM) GetCollection(name string) (*Collection, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	collection, ok := f.Collections[name]
	return collection, ok
}

// GetCollections returns a slice of all collections.
func (f *FSM) GetCollections() []*Collection {
	f.mu.RLock()
	defer f.mu.RUnlock()
	collections := make([]*Collection, 0, len(f.Collections))
	for _, c := range f.Collections {
		collections = append(collections, c)
	}
	return collections
}
