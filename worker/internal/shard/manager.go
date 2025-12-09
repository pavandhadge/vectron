package shard

import (
	"log"
	"sync"

	"github.com/lni/dragonboat/v3"
	"github.com/lni/dragonboat/v3/config"
	sm "github.com/lni/dragonboat/v3/statemachine"
	"github.com/pavandhadge/vectron/worker/internal/pd"
)

// Manager is responsible for managing the lifecycle of shard replicas on a worker node.
type Manager struct {
	nodeHost      *dragonboat.NodeHost
	workerDataDir string
	nodeID        uint64

	mu              sync.RWMutex
	runningReplicas map[uint64]bool // Map of shardID -> bool
}

// NewManager creates a new ShardManager.
func NewManager(nh *dragonboat.NodeHost, workerDataDir string, nodeID uint64) *Manager {
	return &Manager{
		nodeHost:        nh,
		workerDataDir:   workerDataDir,
		nodeID:          nodeID,
		runningReplicas: make(map[uint64]bool),
	}
}

// SyncShards compares the desired shard assignments from the PD with the
// currently running replicas and starts/stops replicas as needed.
func (m *Manager) SyncShards(assignments []*pd.ShardAssignment) {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("ShardManager: Syncing %d assignments.", len(assignments))

	desiredShards := make(map[uint64]*pd.ShardAssignment)
	for _, assignment := range assignments {
		desiredShards[assignment.ShardInfo.ShardID] = assignment
	}

	// Identify shards to stop
	shardsToStop := []uint64{}
	for shardID := range m.runningReplicas {
		if _, exists := desiredShards[shardID]; !exists {
			shardsToStop = append(shardsToStop, shardID)
		}
	}

	// Identify shards to start
	shardsToStart := []*pd.ShardAssignment{}
	for shardID, assignment := range desiredShards {
		if _, running := m.runningReplicas[shardID]; !running {
			shardsToStart = append(shardsToStart, assignment)
		}
	}

	// Stop old replicas
	for _, shardID := range shardsToStop {
		log.Printf("ShardManager: Stopping replica for shard %d", shardID)
		if err := m.nodeHost.StopCluster(shardID); err != nil {
			log.Printf("ShardManager: Failed to stop replica for shard %d: %v", shardID, err)
		}
		delete(m.runningReplicas, shardID)
	}

	// Start new replicas
	for _, assignment := range shardsToStart {
		shardID := assignment.ShardInfo.ShardID
		log.Printf("ShardManager: Starting replica for shard %d with initial members %v", shardID, assignment.InitialMembers)

		rc := config.Config{
			NodeID:             m.nodeID,
			ClusterID:          shardID,
			ElectionRTT:        10,
			HeartbeatRTT:       1,
			CheckQuorum:        true,
			SnapshotEntries:    100,
			CompactionOverhead: 50,
		}

		createFSM := func(clusterID uint64, nodeID uint64) sm.IOnDiskStateMachine {
			sm, err := NewStateMachine(clusterID, nodeID, m.workerDataDir, assignment.ShardInfo.Dimension, assignment.ShardInfo.Distance)
			if err != nil {
				log.Panicf("failed to create state machine: %v", err)
			}
			return sm
		}

		if err := m.nodeHost.StartOnDiskCluster(assignment.InitialMembers, false, createFSM, rc); err != nil {
			log.Printf("ShardManager: Failed to start replica for shard %d: %v", shardID, err)
			continue
		}

		m.runningReplicas[shardID] = true
	}
}

func (m *Manager) IsShardReady(shardID uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.runningReplicas[shardID] {
		log.Printf("IsShardReady: shard %d not in running replicas", shardID)
		return false
	}
	leaderID, _, err := m.nodeHost.GetLeaderID(shardID)
	if err != nil {
		log.Printf("IsShardReady: GetLeaderID for shard %d returned error: %v", shardID, err)
		return false
	}
	if leaderID == 0 {
		log.Printf("IsShardReady: shard %d has no leader", shardID)
		return false
	}
	log.Printf("IsShardReady: shard %d is ready with leader %d", shardID, leaderID)
	return true
}
