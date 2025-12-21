// This file provides a placeholder Finite State Machine (FSM).
// It is a temporary, no-op implementation of the `IOnDiskStateMachine` interface.
// This allows the shard management and Raft logic to be developed and tested
// without a dependency on the full storage engine. It will be replaced by the
// real state machine that interacts with PebbleDB and the HNSW index.

package shard

import (
	"io"

	sm "github.com/lni/dragonboat/v3/statemachine"
)

// PlaceholderFSM is a no-op implementation of `IOnDiskStateMachine`.
// It is used as a temporary stand-in for the real storage-backed FSM.
type PlaceholderFSM struct {
	ClusterID uint64
	NodeID    uint64
}

// NewPlaceholderFSM is the factory function for the placeholder FSM.
func NewPlaceholderFSM(clusterID uint64, nodeID uint64) sm.IOnDiskStateMachine {
	return &PlaceholderFSM{
		ClusterID: clusterID,
		NodeID:    nodeID,
	}
}

// Open is a no-op implementation.
func (f *PlaceholderFSM) Open(stopc <-chan struct{}) (uint64, error) {
	return 0, nil
}

// Update is a no-op implementation that simply returns the length of the command.
func (f *PlaceholderFSM) Update(entries []sm.Entry) ([]sm.Entry, error) {
	for i := range entries {
		entries[i].Result = sm.Result{Value: uint64(len(entries[i].Cmd))}
	}
	return entries, nil
}

// Lookup is a no-op implementation that echoes the query back.
func (f *PlaceholderFSM) Lookup(query interface{}) (interface{}, error) {
	return query, nil
}

// Sync is a no-op implementation.
func (f *PlaceholderFSM) Sync() error {
	return nil
}

// PrepareSnapshot is a no-op implementation.
func (f *PlaceholderFSM) PrepareSnapshot() (interface{}, error) {
	return nil, nil
}

// SaveSnapshot is a no-op implementation.
func (f *PlaceholderFSM) SaveSnapshot(ctx interface{}, w io.Writer, done <-chan struct{}) error {
	return nil
}

// RecoverFromSnapshot is a no-op implementation.
func (f *PlaceholderFSM) RecoverFromSnapshot(r io.Reader, done <-chan struct{}) error {
	return nil
}

// Close is a no-op implementation.
func (f *PlaceholderFSM) Close() error {
	return nil
}

// GetHash is a no-op implementation.
func (f *PlaceholderFSM) GetHash() (uint64, error) {
	return 0, nil
}
