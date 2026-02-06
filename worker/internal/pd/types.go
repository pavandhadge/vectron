// This file defines the data structures that the worker receives from the
// placement driver. These are essentially DTOs (Data Transfer Objects)
// that represent the instructions for the worker, such as shard assignments.
// They are deserialized from the heartbeat response.

package pd

// ShardAssignment contains all the information a worker needs to create or update
// its view of a shard replica.
type ShardAssignment struct {
	ShardInfo      *ShardInfo        `json:"shard_info"`      // The metadata for the shard.
	InitialMembers map[uint64]string `json:"initial_members"` // A map of node IDs to Raft addresses for all replicas in the shard's Raft group.
	Bootstrap      bool              `json:"bootstrap"`       // Whether this replica should bootstrap the shard.
}

// ShardInfo holds the metadata for a single shard.
type ShardInfo struct {
	ShardID       uint64   `json:"shard_id"`        // The unique ID for this shard.
	Collection    string   `json:"collection"`      // The collection this shard belongs to.
	KeyRangeStart uint64   `json:"key_range_start"` // The start of the hash range for this shard.
	KeyRangeEnd   uint64   `json:"key_range_end"`   // The end of the hash range for this shard.
	Replicas      []uint64 `json:"replicas"`        // A slice of worker node IDs that host this shard's replicas.
	LeaderID      uint64   `json:"leader_id"`       // The ID of the current leader of the shard's Raft group.
	Dimension     int32    `json:"dimension"`       // The dimension of the vectors in this shard.
	Distance      string   `json:"distance"`        // The distance metric used for this shard.
	Bootstrapped  bool     `json:"bootstrapped"`    // Whether the raft group has been bootstrapped.
}
