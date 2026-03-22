// lockfree.go - Lock-free read access to FSM state with lazy snapshots
//
// This file provides lock-free read access to the FSM by using
// atomic snapshots. Snapshots are created lazily only when needed.

package fsm

import (
	"sync/atomic"
	"time"
)

// FSMSnapshot is an immutable snapshot of the FSM state
type FSMSnapshot struct {
	Peers            map[string]PeerInfo
	Workers          map[uint64]WorkerInfo
	Rerankers        map[uint64]RerankerInfo
	Collections      map[string]*Collection
	AssignmentsEpoch uint64
	NextShardID      uint64
	NextWorkerID     uint64
	NextRerankerID   uint64
	Timestamp        time.Time
}

// LockFreeFSM provides lock-free read access to FSM state with lazy snapshots
type LockFreeFSM struct {
	snapshot        atomic.Pointer[FSMSnapshot]
	dirty           atomic.Bool   // True if FSM has changed since last snapshot
	version         atomic.Uint64 // Incremented on each write
	snapshotVersion atomic.Uint64 // Version when last snapshot was created
}

// NewLockFreeFSM creates a new lock-free FSM wrapper
func NewLockFreeFSM() *LockFreeFSM {
	return &LockFreeFSM{}
}

// MarkDirty marks the FSM as needing a new snapshot
func (l *LockFreeFSM) MarkDirty() {
	l.dirty.Store(true)
	l.version.Add(1)
}

// GetVersion returns the current version number
func (l *LockFreeFSM) GetVersion() uint64 {
	return l.version.Load()
}

// GetSnapshotVersion returns the version when last snapshot was created
func (l *LockFreeFSM) GetSnapshotVersion() uint64 {
	return l.snapshotVersion.Load()
}

// IsDirty returns true if a new snapshot is needed
func (l *LockFreeFSM) IsDirty() bool {
	return l.dirty.Load()
}

// Load returns the current snapshot (lock-free)
func (l *LockFreeFSM) Load() *FSMSnapshot {
	return l.snapshot.Load()
}

// Store atomically updates the snapshot and clears dirty flag
func (l *LockFreeFSM) Store(snapshot *FSMSnapshot, version uint64) {
	if snapshot != nil {
		snapshot.Timestamp = time.Now()
	}
	l.snapshot.Store(snapshot)
	l.snapshotVersion.Store(version)
	l.dirty.Store(false)
}

// GetWorker returns a worker from the current snapshot (lock-free)
func (l *LockFreeFSM) GetWorker(id uint64) (WorkerInfo, bool) {
	snap := l.snapshot.Load()
	if snap == nil {
		return WorkerInfo{}, false
	}
	w, ok := snap.Workers[id]
	return w, ok
}

// GetWorkers returns all workers from the current snapshot (lock-free)
func (l *LockFreeFSM) GetWorkers() []WorkerInfo {
	snap := l.snapshot.Load()
	if snap == nil {
		return nil
	}
	workers := make([]WorkerInfo, 0, len(snap.Workers))
	for _, w := range snap.Workers {
		workers = append(workers, w)
	}
	return workers
}

// GetCollection returns a collection from the current snapshot (lock-free)
func (l *LockFreeFSM) GetCollection(name string) (*Collection, bool) {
	snap := l.snapshot.Load()
	if snap == nil {
		return nil, false
	}
	c, ok := snap.Collections[name]
	return c, ok
}

// GetCollections returns all collections from the current snapshot (lock-free)
func (l *LockFreeFSM) GetCollections() []*Collection {
	snap := l.snapshot.Load()
	if snap == nil {
		return nil
	}
	collections := make([]*Collection, 0, len(snap.Collections))
	for _, c := range snap.Collections {
		collections = append(collections, c)
	}
	return collections
}

// GetAssignmentsEpoch returns the current assignments epoch (lock-free)
func (l *LockFreeFSM) GetAssignmentsEpoch() uint64 {
	snap := l.snapshot.Load()
	if snap == nil {
		return 0
	}
	return snap.AssignmentsEpoch
}

// IsWorkerHealthy checks if a worker is healthy (lock-free)
func (l *LockFreeFSM) IsWorkerHealthy(workerID uint64) bool {
	snap := l.snapshot.Load()
	if snap == nil {
		return false
	}
	worker, ok := snap.Workers[workerID]
	if !ok {
		return false
	}
	return worker.State == WorkerStateReady && time.Since(worker.LastHeartbeat) < 30*time.Second
}

// IsWorkerAssignedToShard checks if a worker is assigned to a shard (lock-free)
func (l *LockFreeFSM) IsWorkerAssignedToShard(workerID uint64, shardID uint64) bool {
	snap := l.snapshot.Load()
	if snap == nil {
		return false
	}
	for _, collection := range snap.Collections {
		if shard, ok := collection.Shards[shardID]; ok {
			for _, replicaID := range shard.Replicas {
				if replicaID == workerID {
					return true
				}
			}
		}
	}
	return false
}
