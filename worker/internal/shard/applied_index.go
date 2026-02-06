package shard

import "sync"

var (
	appliedIndexMu sync.RWMutex
	appliedIndex   = make(map[uint64]uint64)
)

func setAppliedIndex(shardID uint64, index uint64) {
	if index == 0 {
		return
	}
	appliedIndexMu.Lock()
	if index > appliedIndex[shardID] {
		appliedIndex[shardID] = index
	}
	appliedIndexMu.Unlock()
}

// GetAppliedIndex returns the last applied index observed for the shard.
func GetAppliedIndex(shardID uint64) uint64 {
	appliedIndexMu.RLock()
	defer appliedIndexMu.RUnlock()
	return appliedIndex[shardID]
}

