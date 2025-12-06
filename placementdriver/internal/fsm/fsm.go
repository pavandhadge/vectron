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
	// RegisterWorker is the command to register a new worker.
	RegisterWorker CommandType = iota
)

// Command is the command sent to the Raft log.
type Command struct {
	Type    CommandType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// RegisterWorkerPayload is the payload for the RegisterWorker command.
type RegisterWorkerPayload struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

// FSM is the finite state machine for the placement driver.
// It holds the state of the system and applies commands from the Raft log.
type FSM struct {
	Mu          sync.RWMutex
	workers     map[string]WorkerInfo
	collections map[string]CollectionInfo
	WorkerIdx   int
}

// WorkerInfo holds information about a worker.
type WorkerInfo struct {
	ID            string    `json:"id"`
	Address       string    `json:"address"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// CollectionInfo holds information about a collection.
type CollectionInfo struct {
	Name     string `json:"name"`
	WorkerID string `json:"worker_id"`
}

// NewFSM creates a new FSM.
func NewFSM() *FSM {
	return &FSM{
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
	case RegisterWorker:
		var payload RegisterWorkerPayload
		if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
			return fmt.Errorf("failed to unmarshal RegisterWorker payload: %w", err)
		}
		return f.applyRegisterWorker(payload)
	default:
		return fmt.Errorf("unknown command type: %d", cmd.Type)
	}
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

// Snapshot returns a snapshot of the FSM's state.
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	f.Mu.RLock()
	defer f.Mu.RUnlock()

	// Clone the state
	workers := make(map[string]WorkerInfo)
	for k, v := range f.workers {
		workers[k] = v
	}

	collections := make(map[string]CollectionInfo)
	for k, v := range f.collections {
		collections[k] = v
	}

	return &snapshot{
		workers:     workers,
		collections: collections,
	}, nil
}

// Restore restores the FSM's state from a snapshot.
func (f *FSM) Restore(rc io.ReadCloser) error {
	var workers map[string]WorkerInfo
	var collections map[string]CollectionInfo

	if err := json.NewDecoder(rc).Decode(&workers); err != nil {
		return err
	}
	if err := json.NewDecoder(rc).Decode(&collections); err != nil {
		// This will fail if the snapshot is old and does not have collections.
		// We can ignore this error.
	}

	f.Mu.Lock()
	defer f.Mu.Unlock()

	f.workers = workers
	f.collections = collections

	return nil
}

type snapshot struct {
	workers     map[string]WorkerInfo
	collections map[string]CollectionInfo
}

func (s *snapshot) Persist(sink raft.SnapshotSink) error {
	err := func() error {
		// Encode the workers
		if err := json.NewEncoder(sink).Encode(s.workers); err != nil {
			return err
		}

		// Encode the collections
		if err := json.NewEncoder(sink).Encode(s.collections); err != nil {
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
