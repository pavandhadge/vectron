package shard

import (
	"io"

	sm "github.com/lni/dragonboat/v3/statemachine"
)

// PlaceholderFSM is a no-op implementation of IOnDiskStateMachine used for
// demonstrating the shard management logic without needing the full storage engine.
// It will be replaced with the real storage state machine in a later step.
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

// Open is a no-op.
func (f *PlaceholderFSM) Open(stopc <-chan struct{}) (uint64, error) {
	return 0, nil
}

// Update is a no-op.
func (f *PlaceholderFSM) Update(entries []sm.Entry) ([]sm.Entry, error) {
	// In a real implementation, this would apply writes to PebbleDB and HNSW.
	for i := range entries {
		entries[i].Result = sm.Result{Value: uint64(len(entries[i].Cmd))}
	}
	return entries, nil
}

// Lookup is a no-op.
func (f *PlaceholderFSM) Lookup(query interface{}) (interface{}, error) {
	// In a real implementation, this would perform reads from the storage engine.
	return query, nil
}

// Sync is a no-op.
func (f *PlaceholderFSM) Sync() error {
	return nil
}

// PrepareSnapshot is a no-op.
func (f *PlaceholderFSM) PrepareSnapshot() (interface{}, error) {
	return nil, nil
}

// SaveSnapshot is a no-op.
func (f *PlaceholderFSM) SaveSnapshot(ctx interface{}, w io.Writer, done <-chan struct{}) error {
	return nil
}

// RecoverFromSnapshot is a no-op.
func (f *PlaceholderFSM) RecoverFromSnapshot(r io.Reader, done <-chan struct{}) error {
	return nil
}

// Close is a no-op.
func (f *PlaceholderFSM) Close() error {
	return nil
}

// GetHash is a no-op.
func (f *PlaceholderFSM) GetHash() (uint64, error) {
	return 0, nil
}
