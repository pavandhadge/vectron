package shard

import "sync"

// appliedIndex is a lock-free map for tracking applied indices per shard.
// Uses sync.Map for better concurrent access than a mutex-protected map.
var appliedIndex sync.Map

func setAppliedIndex(shardID uint64, index uint64) {
	if index == 0 {
		return
	}
	// Load current value and only update if new index is greater
	for {
		current, loaded := appliedIndex.Load(shardID)
		if !loaded || index > current.(uint64) {
			if appliedIndex.CompareAndSwap(shardID, current, index) {
				break
			}
		} else {
			break
		}
	}
}

// GetAppliedIndex returns the last applied index observed for the shard.
// This is a lock-free read operation.
func GetAppliedIndex(shardID uint64) uint64 {
	val, ok := appliedIndex.Load(shardID)
	if !ok {
		return 0
	}
	return val.(uint64)
}

// ResetAppliedIndex clears the applied index for a shard.
// This is used during shard recovery.
func ResetAppliedIndex(shardID uint64) {
	appliedIndex.Delete(shardID)
}
