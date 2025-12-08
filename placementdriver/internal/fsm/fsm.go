package fsm

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	sm "github.com/lni/dragonboat/v4/statemachine"
)

// CommandType is the type of command sent to the Raft log.
type CommandType int

const (
	// RegisterPeer is the command to register a new peer.
	RegisterPeer CommandType = iota
	// RegisterWorker is the command to register a new worker.
	RegisterWorker
	// CreateCollection is the command to create a new collection.
	CreateCollection
	// UpdateWorkerHeartbeat is the command to update a worker's heartbeat.
	UpdateWorkerHeartbeat
)

// Command is the command sent to the Raft log.
type Command struct {
	Type    CommandType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// RegisterPeerPayload is the payload for the RegisterPeer command.
type RegisterPeerPayload struct {
	ID       string `json:"id"`
	RaftAddr string `json:"raft_addr"`
	APIAddr  string `json:"api_addr"`
}

// RegisterWorkerPayload is the payload for the RegisterWorker command.
type RegisterWorkerPayload struct {
	Address string `json:"address"`
}

// CreateCollectionPayload is the payload for the CreateCollection command.
type CreateCollectionPayload struct {
	Name          string `json:"name"`
	Dimension     int32  `json:"dimension"`
	Distance      string `json:"distance"`
	InitialShards int    `json:"initial_shards"`
}

// UpdateWorkerHeartbeatPayload is the payload for the UpdateWorkerHeartbeat command.
type UpdateWorkerHeartbeatPayload struct {
	WorkerID uint64 `json:"worker_id"`
}

// Shard and Collection Data Structures
// ======================================================================================

// ShardAssignment contains all info a worker needs to manage a shard replica.
// This is a DTO and is not stored in the FSM state directly.
type ShardAssignment struct {
	ShardInfo      *ShardInfo        `json:"shard_info"`
	InitialMembers map[uint64]string `json:"initial_members"` // map[nodeID]raftAddress
}

// ShardInfo holds the metadata for a single shard.
type ShardInfo struct {
	ShardID       uint64   `json:"shard_id"`
	Collection    string   `json:"collection"`
	KeyRangeStart uint64   `json:"key_range_start"`
	KeyRangeEnd   uint64   `json:"key_range_end"`
	Replicas      []uint64 `json:"replicas"` // Slice of worker node IDs
	LeaderID      uint64   `json:"leader_id"`
	Dimension     int32    `json:"dimension"`
	Distance      string   `json:"distance"`
}

// Collection holds the metadata for a single collection, including its shards.
type Collection struct {
	Name      string                `json:"name"`
	Dimension int32                 `json:"dimension"`
	Distance  string                `json:"distance"`
	Shards    map[uint64]*ShardInfo `json:"shards"` // map[shardID]*ShardInfo
}

// ======================================================================================
// FSM Implementation
// ======================================================================================

// FSM is the finite state machine for the placement driver.
type FSM struct {
	mu           sync.RWMutex
	Peers        map[string]PeerInfo    // nodeID -> PeerInfo
	Workers      map[uint64]WorkerInfo  // workerID -> WorkerInfo
	Collections  map[string]*Collection // map[collectionName]*Collection
	NextShardID  uint64
	NextWorkerID uint64
}

// PeerInfo holds information about a peer in the raft cluster.
type PeerInfo struct {
	ID       string `json:"id"`
	RaftAddr string `json:"raft_addr"`
	APIAddr  string `json:"api_addr"`
}

// WorkerInfo holds information about a worker.
type WorkerInfo struct {
	ID            uint64    `json:"id"`
	Address       string    `json:"address"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// NewFSM creates a new FSM.
func NewFSM() *FSM {
	return &FSM{
		Peers:        make(map[string]PeerInfo),
		Workers:      make(map[uint64]WorkerInfo),
		Collections:  make(map[string]*Collection),
		NextShardID:  1,
		NextWorkerID: 1,
	}
}

// Update applies commands from the Raft log to the FSM.
func (f *FSM) Update(entries []sm.Entry) ([]sm.Entry, error) {
	for i, entry := range entries {
		var cmd Command
		if err := json.Unmarshal(entry.Cmd, &cmd); err != nil {
			return nil, fmt.Errorf("failed to unmarshal command: %w", err)
		}

		var appErr error
		var result uint64
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
				appErr = f.applyCreateCollection(payload)
			}
		case UpdateWorkerHeartbeat:
			var payload UpdateWorkerHeartbeatPayload
			if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
				appErr = fmt.Errorf("failed to unmarshal UpdateWorkerHeartbeat payload: %w", err)
			} else {
				f.applyUpdateWorkerHeartbeat(payload)
			}
		default:
			appErr = fmt.Errorf("unknown command type: %d", cmd.Type)
		}

		if appErr != nil {
			fmt.Printf("Error applying command: %v\n", appErr)
			// For queries that return a result, 0 or a specific error code would be appropriate.
			entries[i].Result = sm.Result{Value: 0}
		} else {
			entries[i].Result = sm.Result{Value: result}
		}
	}
	return entries, nil
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

	workerID := f.NextWorkerID
	f.NextWorkerID++

	f.Workers[workerID] = WorkerInfo{
		ID:            workerID,
		Address:       payload.Address,
		LastHeartbeat: time.Now(),
	}
	return workerID
}

func (f *FSM) applyCreateCollection(payload CreateCollectionPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.Collections[payload.Name]; ok {
		return fmt.Errorf("collection %s already exists", payload.Name)
	}

	const replicationFactor = 3
	if len(f.Workers) < replicationFactor {
		return fmt.Errorf("not enough workers (%d) to meet replication factor (%d)", len(f.Workers), replicationFactor)
	}

	// Create the collection.
	collection := &Collection{
		Name:      payload.Name,
		Dimension: payload.Dimension,
		Distance:  payload.Distance,
		Shards:    make(map[uint64]*ShardInfo),
	}

	// Create initial shards.
	numShards := payload.InitialShards
	if numShards <= 0 {
		numShards = 1 // Default to at least one shard
	}
	shardRangeSize := uint64(math.MaxUint64 / float64(numShards))

	// Get a list of worker IDs to pick replicas from.
	workerIDs := make([]uint64, 0, len(f.Workers))
	for _, w := range f.Workers {
		workerIDs = append(workerIDs, w.ID)
	}

	workerIdx := 0
	for i := 0; i < numShards; i++ {
		shardID := f.NextShardID
		f.NextShardID++

		startKey := uint64(i) * shardRangeSize
		endKey := (uint64(i+1) * shardRangeSize) - 1
		if i == numShards-1 {
			endKey = math.MaxUint64
		}

		// Assign replicas.
		replicas := make([]uint64, 0, replicationFactor)
		for j := 0; j < replicationFactor; j++ {
			replicas = append(replicas, workerIDs[workerIdx%len(workerIDs)])
			workerIdx++
		}

		shard := &ShardInfo{
			ShardID:       shardID,
			Collection:    payload.Name,
			KeyRangeStart: startKey,
			KeyRangeEnd:   endKey,
			Replicas:      replicas,
			Dimension:     payload.Dimension,
			Distance:      payload.Distance,
		}
		collection.Shards[shardID] = shard
	}

	f.Collections[collection.Name] = collection
	fmt.Printf("Created collection '%s' with %d shards\n", collection.Name, numShards)
	return nil
}

func (f *FSM) applyUpdateWorkerHeartbeat(payload UpdateWorkerHeartbeatPayload) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if worker, ok := f.Workers[payload.WorkerID]; ok {
		worker.LastHeartbeat = time.Now()
		f.Workers[payload.WorkerID] = worker
	}
}

// Lookup is used for read-only queries of the FSM.
func (f *FSM) Lookup(query interface{}) (interface{}, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return &fsmSnapshot{
		Peers:        f.Peers,
		Workers:      f.Workers,
		Collections:  f.Collections,
		NextShardID:  f.NextShardID,
		NextWorkerID: f.NextWorkerID,
	}, nil
}

// fsmSnapshot is a struct to hold all the data for snapshotting.
type fsmSnapshot struct {
	Peers        map[string]PeerInfo    `json:"peers"`
	Workers      map[uint64]WorkerInfo  `json:"workers"`
	Collections  map[string]*Collection `json:"collections"`
	NextShardID  uint64                 `json:"next_shard_id"`
	NextWorkerID uint64                 `json:"next_worker_id"`
}

// SaveSnapshot saves the FSM state to a snapshot.
func (f *FSM) SaveSnapshot(w io.Writer, fc sm.ISnapshotFileCollection, done <-chan struct{}) error {
	f.mu.RLock()
	defer f.mu.RUnlock()

	data := &fsmSnapshot{
		Peers:        f.Peers,
		Workers:      f.Workers,
		Collections:  f.Collections,
		NextShardID:  f.NextShardID,
		NextWorkerID: f.NextWorkerID,
	}

	return json.NewEncoder(w).Encode(data)
}

// RecoverFromSnapshot restores the FSM state from a snapshot.
func (f *FSM) RecoverFromSnapshot(r io.Reader, files []sm.SnapshotFile, done <-chan struct{}) error {
	var data fsmSnapshot
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.Peers = data.Peers
	f.Workers = data.Workers
	f.Collections = data.Collections
	f.NextShardID = data.NextShardID
	f.NextWorkerID = data.NextWorkerID

	return nil
}

// Close closes the FSM.
func (f *FSM) Close() error {
	return nil
}

// ======================================================================================
// Helper methods for accessing state
// ======================================================================================

// GetPeer is a helper method for accessing peer info.
func (f *FSM) GetPeer(id string) (PeerInfo, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	peer, ok := f.Peers[id]
	return peer, ok
}

// GetWorker is a helper method for accessing worker info.
func (f *FSM) GetWorker(id uint64) (WorkerInfo, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	worker, ok := f.Workers[id]
	return worker, ok
}

// GetWorkers is a helper method for accessing worker info.
func (f *FSM) GetWorkers() []WorkerInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var workers []WorkerInfo
	for _, w := range f.Workers {
		workers = append(workers, w)
	}
	return workers
}

// GetCollection returns a collection from the FSM.
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
	var collections []*Collection
	for _, c := range f.Collections {
		collections = append(collections, c)
	}
	return collections
}
