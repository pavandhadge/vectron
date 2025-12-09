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

// SaveSnapshot is a no-op.
func (f *PlaceholderFSM) SaveSnapshot(w io.Writer, fc sm.ISnapshotFileCollection, done <-chan struct{}) error {
	return nil
}

// RecoverFromSnapshot is a no-op.
func (f *PlaceholderFSM) RecoverFromSnapshot(r io.Reader, files []sm.SnapshotFile, done <-chan struct{}) error {
	return nil
}

// Close is a no-op.
func (f *PlaceholderFSM) Close() error {
	return nil
}
