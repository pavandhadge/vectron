package pd

// This file contains copies of the data structures that are shared between
// the placementdriver and the worker. This is to avoid cross-package imports.

// ShardAssignment contains all info a worker needs to manage a shard replica.
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
