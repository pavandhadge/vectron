// This file implements the ShardManager, which is responsible for managing the
// lifecycle of all shard replicas on a single worker node. It communicates with
// the placement driver to get the desired state of shards and reconciles it
// with the current state by starting and stopping Raft clusters (shards).

package shard

import (
	"log"
	"sync"

	"github.com/lni/dragonboat/v3"
	"github.com/lni/dragonboat/v3/config"
	sm "github.com/lni/dragonboat/v3/statemachine"
	"github.com/pavandhadge/vectron/shared/proto/placementdriver"
	"github.com/pavandhadge/vectron/worker/internal/pd"
)

// Manager is responsible for managing the lifecycle of shard replicas on a worker node.
type Manager struct {
	nodeHost      *dragonboat.NodeHost // The Dragonboat instance that runs all Raft clusters.
	workerDataDir string               // The root directory for this worker's data.
	nodeID        uint64               // The ID of this worker node.

	mu              sync.RWMutex
	runningReplicas map[uint64]bool // A set of shard IDs for replicas currently running on this node.
}

// NewManager creates a new instance of the ShardManager.
func NewManager(nh *dragonboat.NodeHost, workerDataDir string, nodeID uint64) *Manager {
	return &Manager{
		nodeHost:        nh,
		workerDataDir:   workerDataDir,
		nodeID:          nodeID,
		runningReplicas: make(map[uint64]bool),
	}
}

// SyncShards is the core reconciliation loop. It compares the desired shard
// assignments from the placement driver with the currently running replicas
// and starts or stops replicas as needed.
func (m *Manager) SyncShards(assignments []*pd.ShardAssignment) {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("ShardManager: Syncing %d assignments.", len(assignments))

	desiredShards := make(map[uint64]*pd.ShardAssignment)
	for _, assignment := range assignments {
		desiredShards[assignment.ShardInfo.ShardID] = assignment
	}

	// Phase 1: Identify shards to stop.
	// These are replicas that are running locally but are no longer in the desired state from the PD.
	shardsToStop := []uint64{}
	for shardID := range m.runningReplicas {
		if _, exists := desiredShards[shardID]; !exists {
			shardsToStop = append(shardsToStop, shardID)
		}
	}

	// Phase 2: Identify shards to start.
	// These are replicas that are in the desired state but not currently running locally.
	shardsToStart := []*pd.ShardAssignment{}
	for shardID, assignment := range desiredShards {
		if _, running := m.runningReplicas[shardID]; !running {
			shardsToStart = append(shardsToStart, assignment)
		}
	}

	// Phase 3: Execute the changes.

	// Stop old/unwanted replicas.
	for _, shardID := range shardsToStop {
		log.Printf("ShardManager: Stopping replica for shard %d", shardID)
		if err := m.nodeHost.StopCluster(shardID); err != nil {
			log.Printf("ShardManager: Failed to stop replica for shard %d: %v", shardID, err)
			// Continue even if stopping fails.
		}
		delete(m.runningReplicas, shardID)
	}

	// Start new replicas.
	for _, assignment := range shardsToStart {
		shardID := assignment.ShardInfo.ShardID
		log.Printf("ShardManager: Starting replica for shard %d with initial members %v", shardID, assignment.InitialMembers)

		// Configure the new Raft cluster for the shard.
		rc := config.Config{
			NodeID:             m.nodeID,
			ClusterID:          shardID,
			ElectionRTT:        10,
			HeartbeatRTT:       1,
			CheckQuorum:        true,
			SnapshotEntries:    100,
			CompactionOverhead: 50,
		}

		// The factory function that Dragonboat will call to create the shard's state machine.
		createFSM := func(clusterID uint64, nodeID uint64) sm.IOnDiskStateMachine {
			stateMachine, err := NewStateMachine(clusterID, nodeID, m.workerDataDir, assignment.ShardInfo.Dimension, assignment.ShardInfo.Distance)
			if err != nil {
				// Panicking here because a failure to create a state machine is a fatal error for the worker.
				log.Panicf("failed to create state machine for shard %d: %v", clusterID, err)
			}
			return stateMachine
		}

		// Start the new Raft cluster.
		if err := m.nodeHost.StartOnDiskCluster(assignment.InitialMembers, false, createFSM, rc); err != nil {
			log.Printf("ShardManager: Failed to start replica for shard %d: %v", shardID, err)
			continue
		}

		m.runningReplicas[shardID] = true
	}
}

// IsShardReady checks if a specific shard is ready to be used on this node.
// A shard is considered ready if it is running and has a leader.
func (m *Manager) IsShardReady(shardID uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.runningReplicas[shardID] {
		log.Printf("IsShardReady: Shard %d is not in the set of running replicas.", shardID)
		return false
	}

	leaderID, _, err := m.nodeHost.GetLeaderID(shardID)
	if err != nil {
		log.Printf("IsShardReady: Error getting leader for shard %d: %v", shardID, err)
		return false
	}
	if leaderID == 0 {
		log.Printf("IsShardReady: Shard %d currently has no leader.", shardID)
		return false
	}

	log.Printf("IsShardReady: Shard %d is ready with leader %d.", shardID, leaderID)
	return true
}

// GetShardLeaderInfo returns a list of ShardLeaderInfo for all running replicas.
func (m *Manager) GetShardLeaderInfo() []*placementdriver.ShardLeaderInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	leaderInfo := make([]*placementdriver.ShardLeaderInfo, 0, len(m.runningReplicas))
	for shardID := range m.runningReplicas {
		leaderID, _, err := m.nodeHost.GetLeaderID(shardID)
		if err != nil {
			// Log the error but don't stop. We can still report the leaders for other shards.
			log.Printf("Error getting leader for shard %d: %v", shardID, err)
			continue
		}
		leaderInfo = append(leaderInfo, &placementdriver.ShardLeaderInfo{
			ShardId:  shardID,
			LeaderId: leaderID,
		})
	}
	return leaderInfo
}

// GetShards returns the IDs of all shard replicas currently running on this node.
func (m *Manager) GetShards() []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]uint64, 0, len(m.runningReplicas))
	for id := range m.runningReplicas {
		ids = append(ids, id)
	}
	return ids
}

