package fsm

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/hashicorp/raft"
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
	ID      string `json:"id"`
	Address string `json:"address"`
}

// CreateCollectionPayload is the payload for the CreateCollection command.
type CreateCollectionPayload struct {
	Name      string `json:"name"`
	Dimension int32  `json:"dimension"`
	Distance  string `json:"distance"`
	WorkerID  string `json:"worker_id"`
}

// FSM is the finite state machine for the placement driver.
// It holds the state of the system and applies commands from the Raft log.
type FSM struct {
	Mu          sync.RWMutex
	peers       map[string]PeerInfo // nodeID -> PeerInfo
	workers     map[string]WorkerInfo
	collections map[string]CollectionInfo
}

// PeerInfo holds information about a peer in the raft cluster.
type PeerInfo struct {
	ID       string `json:"id"`
	RaftAddr string `json:"raft_addr"`
	APIAddr  string `json:"api_addr"`
}

// WorkerInfo holds information about a worker.
type WorkerInfo struct {
	ID            string    `json:"id"`
	Address       string    `json:"address"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// CollectionInfo holds information about a collection.
type CollectionInfo struct {
	Name      string `json:"name"`
	WorkerID  string `json:"worker_id"`
	Dimension int32  `json:"dimension"`
	Distance  string `json:"distance"`
}

// NewFSM creates a new FSM.
func NewFSM() *FSM {
	return &FSM{
		peers:       make(map[string]PeerInfo),
		workers:     make(map[string]WorkerInfo),
		collections: make(map[string]CollectionInfo),
	}
}

// Apply applies a command to the FSM.
// This is the only way to modify the state of the FSM.
func (f *FSM) Apply(log *raft.Log) interface{} {
	var cmd Command
	if err := json.Unmarshal(log.Data, &cmd); err != nil {
		return fmt.Errorf("failed to unmarshal command: %w", err)
	}

	switch cmd.Type {
	case RegisterPeer:
		var payload RegisterPeerPayload
		if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
			return fmt.Errorf("failed to unmarshal RegisterPeer payload: %w", err)
		}
		return f.applyRegisterPeer(payload)
	case RegisterWorker:
		var payload RegisterWorkerPayload
		if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
			return fmt.Errorf("failed to unmarshal RegisterWorker payload: %w", err)
		}
		return f.applyRegisterWorker(payload)
	case CreateCollection:
		var payload CreateCollectionPayload
		if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
			return fmt.Errorf("failed to unmarshal CreateCollection payload: %w", err)
		}
		return f.applyCreateCollection(payload)
	default:
		return fmt.Errorf("unknown command type: %d", cmd.Type)
	}
}

func (f *FSM) applyRegisterPeer(payload RegisterPeerPayload) interface{} {
	f.Mu.Lock()
	defer f.Mu.Unlock()

	f.peers[payload.ID] = PeerInfo{
		ID:       payload.ID,
		RaftAddr: payload.RaftAddr,
		APIAddr:  payload.APIAddr,
	}

	return nil
}

func (f *FSM) applyRegisterWorker(payload RegisterWorkerPayload) interface{} {
	f.Mu.Lock()
	defer f.Mu.Unlock()

	f.workers[payload.ID] = WorkerInfo{
		ID:            payload.ID,
		Address:       payload.Address,
		LastHeartbeat: time.Now(),
	}

	return nil
}

func (f *FSM) applyCreateCollection(payload CreateCollectionPayload) interface{} {
	f.Mu.Lock()
	defer f.Mu.Unlock()

	if _, ok := f.collections[payload.Name]; ok {
		return fmt.Errorf("collection %s already exists", payload.Name)
	}

	f.collections[payload.Name] = CollectionInfo{
		Name:      payload.Name,
		WorkerID:  payload.WorkerID,
		Dimension: payload.Dimension,
		Distance:  payload.Distance,
	}

	return nil
}

// GetPeer returns a peer from the FSM.
func (f *FSM) GetPeer(id string) (PeerInfo, bool) {
	f.Mu.RLock()
	defer f.Mu.RUnlock()

	peer, ok := f.peers[id]
	return peer, ok
}

// GetWorker returns a worker from the FSM.
func (f *FSM) GetWorker(id string) (WorkerInfo, bool) {
	f.Mu.RLock()
	defer f.Mu.RUnlock()

	worker, ok := f.workers[id]
	return worker, ok
}

// GetWorkers returns a slice of all workers.
func (f *FSM) GetWorkers() []WorkerInfo {
	f.Mu.RLock()
	defer f.Mu.RUnlock()

	workers := make([]WorkerInfo, 0, len(f.workers))
	for _, worker := range f.workers {
		workers = append(workers, worker)
	}
	return workers
}

// GetCollection returns a collection from the FSM.
func (f *FSM) GetCollection(name string) (CollectionInfo, bool) {
	f.Mu.RLock()
	defer f.Mu.RUnlock()

	collection, ok := f.collections[name]
	return collection, ok
}

// GetCollections returns a slice of all collections.
func (f *FSM) GetCollections() []CollectionInfo {
	f.Mu.RLock()
	defer f.Mu.RUnlock()

	collections := make([]CollectionInfo, 0, len(f.collections))
	for _, c := range f.collections {
		collections = append(collections, c)
	}
	return collections
}

// fsmSnapshot is a struct to hold all the data for snapshotting.
type fsmSnapshot struct {
	Peers       map[string]PeerInfo       `json:"peers"`
	Workers     map[string]WorkerInfo     `json:"workers"`
	Collections map[string]CollectionInfo `json:"collections"`
}

// Snapshot returns a snapshot of the FSM's state.
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	f.Mu.RLock()
	defer f.Mu.RUnlock()

	data := &fsmSnapshot{
		Peers:       make(map[string]PeerInfo),
		Workers:     make(map[string]WorkerInfo),
		Collections: make(map[string]CollectionInfo),
	}

	for k, v := range f.peers {
		data.Peers[k] = v
	}
	for k, v := range f.workers {
		data.Workers[k] = v
	}
	for k, v := range f.collections {
		data.Collections[k] = v
	}

	return &snapshot{data: data}, nil
}

// Restore restores the FSM's state from a snapshot.
func (f *FSM) Restore(rc io.ReadCloser) error {
	var data fsmSnapshot
	if err := json.NewDecoder(rc).Decode(&data); err != nil {
		return err
	}

	f.Mu.Lock()
	defer f.Mu.Unlock()

	f.peers = data.Peers
	f.workers = data.Workers
	f.collections = data.Collections

	return nil
}

type snapshot struct {
	data *fsmSnapshot
}

func (s *snapshot) Persist(sink raft.SnapshotSink) error {
	err := func() error {
		if err := json.NewEncoder(sink).Encode(s.data); err != nil {
			return err
		}
		return nil
	}()

	if err != nil {
		sink.Cancel()
	}

	return err
}

func (s *snapshot) Release() {}
