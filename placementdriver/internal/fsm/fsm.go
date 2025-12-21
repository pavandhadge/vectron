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
	WorkerID uint64 `json:"worker_id"`
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

// WorkerInfo holds information about a registered worker node.
type WorkerInfo struct {
	ID            uint64    `json:"id"`
	GrpcAddress   string    `json:"grpc_address"`
	RaftAddress   string    `json:"raft_address"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
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

	workerID := f.NextWorkerID
	f.NextWorkerID++

	f.Workers[workerID] = WorkerInfo{
		ID:            workerID,
		GrpcAddress:   payload.GrpcAddress,
		RaftAddress:   payload.RaftAddress,
		LastHeartbeat: time.Now().UTC(),
	}
	fmt.Printf("Registered new worker %d with GRPC address %s and Raft address %s\n", workerID, payload.GrpcAddress, payload.RaftAddress)
	return workerID
}

func (f *FSM) applyCreateCollection(payload CreateCollectionPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	fmt.Printf("Attempting to create collection '%s'. Workers available: %d\n", payload.Name, len(f.Workers))

	if _, ok := f.Collections[payload.Name]; ok {
		return fmt.Errorf("collection %s already exists", payload.Name)
	}

	const replicationFactor = 1 // TODO: Make this configurable per collection.
	if len(f.Workers) < replicationFactor {
		err := fmt.Errorf("not enough workers (%d) to meet replication factor (%d)", len(f.Workers), replicationFactor)
		fmt.Printf("Error creating collection: %v\n", err)
		return err
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

	workerIDs := make([]uint64, 0, len(f.Workers))
	for id := range f.Workers {
		workerIDs = append(workerIDs, id)
	}

	// Assign replicas to shards in a round-robin fashion.
	workerIdx := 0
	for i := 0; i < numShards; i++ {
		shardID := f.NextShardID
		f.NextShardID++

		startKey := uint64(i) * shardRangeSize
		endKey := (uint64(i+1) * shardRangeSize) - 1
		if i == numShards-1 {
			endKey = math.MaxUint64 // Ensure the last shard covers the rest of the key space.
		}

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
	fmt.Printf("FSM: Successfully created collection '%s' with %d shards\n", collection.Name, numShards)
	return nil
}

func (f *FSM) applyUpdateWorkerHeartbeat(payload UpdateWorkerHeartbeatPayload) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if worker, ok := f.Workers[payload.WorkerID]; ok {
		worker.LastHeartbeat = time.Now().UTC()
		f.Workers[payload.WorkerID] = worker
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
		NextShardID:  f.NextShardID,
		NextWorkerID: f.NextWorkerID,
	}, nil
}

// fsmSnapshot is a struct used for serializing the FSM state for snapshotting.
type fsmSnapshot struct {
	Peers        map[string]PeerInfo    `json:"peers"`
	Workers      map[uint64]WorkerInfo  `json:"workers"`
	Collections  map[string]*Collection `json:"collections"`
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
	f.NextShardID = data.NextShardID
	f.NextWorkerID = data.NextWorkerID

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
