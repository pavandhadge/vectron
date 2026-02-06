// This file implements the ShardManager, which is responsible for managing the
// lifecycle of all shard replicas on a single worker node. It communicates with
// the placement driver to get the desired state of shards and reconciles it
// with the current state by starting and stopping Raft clusters (shards).

package shard

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

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

	mu               sync.RWMutex
	runningReplicas  map[uint64]bool // A set of shard IDs for replicas currently running on this node.
	shardCollections map[uint64]string
	membershipReportedAt map[uint64]time.Time
	shardEpochs      map[uint64]uint64
	pendingLeaderTransfers map[uint64]uint64
}

// NewManager creates a new instance of the ShardManager.
func NewManager(nh *dragonboat.NodeHost, workerDataDir string, nodeID uint64) *Manager {
	return &Manager{
		nodeHost:         nh,
		workerDataDir:    workerDataDir,
		nodeID:           nodeID,
		runningReplicas:  make(map[uint64]bool),
		shardCollections: make(map[uint64]string),
		membershipReportedAt: make(map[uint64]time.Time),
		shardEpochs:      make(map[uint64]uint64),
		pendingLeaderTransfers: make(map[uint64]uint64),
	}
}

// SetNodeID updates the node ID used for shard replication.
func (m *Manager) SetNodeID(nodeID uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodeID = nodeID
}

// SyncShards is the core reconciliation loop. It compares the desired shard
// assignments from the placement driver with the currently running replicas
// and starts or stops replicas as needed.
func (m *Manager) SyncShards(assignments []*pd.ShardAssignment) {
	log.Printf("ShardManager: Syncing %d assignments.", len(assignments))

	desiredShards := make(map[uint64]*pd.ShardAssignment)
	for _, assignment := range assignments {
		desiredShards[assignment.ShardInfo.ShardID] = assignment
	}

	m.updateShardEpochs(desiredShards)

	m.mu.RLock()
	runningSnapshot := make(map[uint64]bool, len(m.runningReplicas))
	for shardID := range m.runningReplicas {
		runningSnapshot[shardID] = true
	}
	m.mu.RUnlock()

	// Phase 1: Identify shards to stop.
	// These are replicas that are running locally but are no longer in the desired state from the PD.
	shardsToStop := []uint64{}
	for shardID := range runningSnapshot {
		if _, exists := desiredShards[shardID]; !exists {
			shardsToStop = append(shardsToStop, shardID)
		}
	}

	// Phase 2: Identify shards to start.
	// These are replicas that are in the desired state but not currently running locally.
	shardsToStart := []*pd.ShardAssignment{}
	for shardID, assignment := range desiredShards {
		if _, running := runningSnapshot[shardID]; !running {
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
	}

	started := make(map[uint64]*pd.ShardAssignment)

	// Start new replicas.
	for _, assignment := range shardsToStart {
		shardID := assignment.ShardInfo.ShardID

		initialMembers := assignment.InitialMembers
		hasData := m.hasLocalShardData(shardID)
		join := false

		if assignment.Bootstrap {
			join = false
			if len(initialMembers) == 0 {
				log.Printf("ShardManager: Missing initial members for bootstrap shard %d. Skipping start.", shardID)
				continue
			}
		} else if hasData {
			initialMembers = nil
			join = false
		} else {
			// Join existing cluster when no local data is present.
			initialMembers = nil
			join = true
		}

		log.Printf("ShardManager: Starting replica for shard %d (join=%v, bootstrapped=%v, bootstrap=%v)", shardID, join, assignment.ShardInfo.Bootstrapped, assignment.Bootstrap)

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
		if err := m.nodeHost.StartOnDiskCluster(initialMembers, join, createFSM, rc); err != nil {
			log.Printf("ShardManager: Failed to start replica for shard %d: %v", shardID, err)
			continue
		}

		started[shardID] = assignment
	}

	m.mu.Lock()
	for _, shardID := range shardsToStop {
		delete(m.runningReplicas, shardID)
		delete(m.shardCollections, shardID)
	}
	for shardID, assignment := range started {
		m.runningReplicas[shardID] = true
		m.shardCollections[shardID] = assignment.ShardInfo.Collection
	}
	m.mu.Unlock()

	// Phase 4: Reconcile membership for running shards (leader-only).
	for _, assignment := range assignments {
		if m.isShardRunning(assignment.ShardInfo.ShardID) {
			m.reconcileMembership(assignment)
		}
	}
}

func (m *Manager) hasLocalShardData(shardID uint64) bool {
	shardDir := filepath.Join(m.workerDataDir, fmt.Sprintf("shard-%d", shardID))
	if _, err := os.Stat(filepath.Join(shardDir, "CURRENT")); err == nil {
		return true
	}
	if _, err := os.Stat(shardDir); err == nil {
		return true
	}
	return false
}

func (m *Manager) isShardRunning(shardID uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.runningReplicas[shardID]
}

func (m *Manager) updateShardEpochs(assignments map[uint64]*pd.ShardAssignment) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for shardID, assignment := range assignments {
		if assignment == nil || assignment.ShardInfo == nil {
			continue
		}
		if assignment.ShardInfo.Epoch > m.shardEpochs[shardID] {
			m.shardEpochs[shardID] = assignment.ShardInfo.Epoch
		}
	}
}

func (m *Manager) getShardEpoch(shardID uint64) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.shardEpochs[shardID]
}

// GetShardEpoch returns the current epoch for a shard.
func (m *Manager) GetShardEpoch(shardID uint64) uint64 {
	return m.getShardEpoch(shardID)
}

func (m *Manager) reconcileMembership(assignment *pd.ShardAssignment) {
	shardID := assignment.ShardInfo.ShardID
	if assignment.ShardInfo.Epoch < m.getShardEpoch(shardID) {
		return
	}
	leaderID, valid, err := m.nodeHost.GetLeaderID(shardID)
	if err != nil || !valid {
		return
	}
	if leaderID != m.nodeID {
		return
	}

	desired := make(map[uint64]struct{}, len(assignment.ShardInfo.Replicas))
	for _, replicaID := range assignment.ShardInfo.Replicas {
		desired[replicaID] = struct{}{}
	}
	if len(desired) == 0 {
		return
	}

	membership, err := m.getMembership(shardID)
	if err != nil {
		log.Printf("ShardManager: Failed to get membership for shard %d: %v", shardID, err)
		return
	}

	if _, ok := desired[m.nodeID]; !ok {
		target := pickLeaderTransferTarget(desired, membership, m.nodeID)
		if target != 0 {
			if err := m.nodeHost.RequestLeaderTransfer(shardID, target); err != nil {
				log.Printf("ShardManager: Leader transfer request failed for shard %d: %v", shardID, err)
			} else {
				m.recordLeaderTransfer(shardID, assignment.ShardInfo.Epoch)
			}
		}
		return
	}

	for nodeID := range desired {
		if _, ok := membership.Nodes[nodeID]; ok {
			continue
		}
		addr := assignment.InitialMembers[nodeID]
		if addr == "" {
			log.Printf("ShardManager: Missing raft address for node %d in shard %d, skipping add", nodeID, shardID)
			continue
		}
		if err := m.requestAddNode(shardID, nodeID, addr, membership.ConfigChangeID); err != nil {
			log.Printf("ShardManager: Failed to add node %d to shard %d: %v", nodeID, shardID, err)
			return
		}
		if membership, err = m.getMembership(shardID); err != nil {
			log.Printf("ShardManager: Failed to refresh membership for shard %d: %v", shardID, err)
			return
		}
	}

	for nodeID := range membership.Nodes {
		if _, ok := desired[nodeID]; ok {
			continue
		}
		if err := m.requestDeleteNode(shardID, nodeID, membership.ConfigChangeID); err != nil {
			log.Printf("ShardManager: Failed to remove node %d from shard %d: %v", nodeID, shardID, err)
			return
		}
		if membership, err = m.getMembership(shardID); err != nil {
			log.Printf("ShardManager: Failed to refresh membership for shard %d: %v", shardID, err)
			return
		}
	}
}

func (m *Manager) getMembership(shardID uint64) (*dragonboat.Membership, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.nodeHost.SyncGetClusterMembership(ctx, shardID)
}

func (m *Manager) requestAddNode(shardID uint64, nodeID uint64, addr string, cfgChangeID uint64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return m.nodeHost.SyncRequestAddNode(ctx, shardID, nodeID, addr, cfgChangeID)
}

func (m *Manager) requestDeleteNode(shardID uint64, nodeID uint64, cfgChangeID uint64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return m.nodeHost.SyncRequestDeleteNode(ctx, shardID, nodeID, cfgChangeID)
}

func pickLeaderTransferTarget(desired map[uint64]struct{}, membership *dragonboat.Membership, self uint64) uint64 {
	for nodeID := range desired {
		if nodeID == self {
			continue
		}
		if _, ok := membership.Nodes[nodeID]; ok {
			return nodeID
		}
	}
	return 0
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

// GetShardsForCollection returns shard IDs for a specific collection.
func (m *Manager) GetShardsForCollection(collection string) []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]uint64, 0, len(m.runningReplicas))
	for id := range m.runningReplicas {
		if m.shardCollections[id] == collection {
			ids = append(ids, id)
		}
	}
	return ids
}

// GetShardMembershipInfo returns membership info for shards where this node is leader.
func (m *Manager) GetShardMembershipInfo() []*placementdriver.ShardMembershipInfo {
	shardIDs := m.GetShards()
	now := time.Now()
	infos := make([]*placementdriver.ShardMembershipInfo, 0)

	for _, shardID := range shardIDs {
		leaderID, valid, err := m.nodeHost.GetLeaderID(shardID)
		if err != nil || !valid || leaderID != m.nodeID {
			continue
		}
		if !m.shouldReportMembership(shardID, now) {
			continue
		}
		membership, err := m.getMembership(shardID)
		if err != nil {
			log.Printf("ShardManager: Failed to get membership for shard %d: %v", shardID, err)
			continue
		}
		nodeIDs := make([]uint64, 0, len(membership.Nodes))
		for nodeID := range membership.Nodes {
			nodeIDs = append(nodeIDs, nodeID)
		}
		infos = append(infos, &placementdriver.ShardMembershipInfo{
			ShardId:        shardID,
			ConfigChangeId: membership.ConfigChangeID,
			NodeIds:        nodeIDs,
		})
	}

	return infos
}

const membershipReportInterval = 15 * time.Second

func (m *Manager) shouldReportMembership(shardID uint64, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	last := m.membershipReportedAt[shardID]
	if !last.IsZero() && now.Sub(last) < membershipReportInterval {
		return false
	}
	m.membershipReportedAt[shardID] = now
	return true
}

// GetShardProgressInfo returns applied index info for running shards.
func (m *Manager) GetShardProgressInfo() []*placementdriver.ShardProgressInfo {
	shardIDs := m.GetShards()
	infos := make([]*placementdriver.ShardProgressInfo, 0, len(shardIDs))
	for _, shardID := range shardIDs {
		infos = append(infos, &placementdriver.ShardProgressInfo{
			ShardId:      shardID,
			AppliedIndex: GetAppliedIndex(shardID),
			ShardEpoch:   m.getShardEpoch(shardID),
		})
	}
	return infos
}

// GetLeaderTransferAcks returns acknowledgements for completed leader transfers.
func (m *Manager) GetLeaderTransferAcks() []*placementdriver.ShardLeaderTransferAck {
	now := time.Now().UnixMilli()
	acks := make([]*placementdriver.ShardLeaderTransferAck, 0)

	m.mu.Lock()
	defer m.mu.Unlock()

	for shardID, epoch := range m.pendingLeaderTransfers {
		leaderID, valid, err := m.nodeHost.GetLeaderID(shardID)
		if err != nil || !valid {
			continue
		}
		if leaderID == m.nodeID {
			continue
		}
		acks = append(acks, &placementdriver.ShardLeaderTransferAck{
			ShardId:          shardID,
			FromNodeId:       m.nodeID,
			ToNodeId:         leaderID,
			ShardEpoch:       epoch,
			TimestampUnixMs:  now,
		})
		delete(m.pendingLeaderTransfers, shardID)
	}

	return acks
}

func (m *Manager) recordLeaderTransfer(shardID uint64, epoch uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev, ok := m.pendingLeaderTransfers[shardID]; ok && prev >= epoch {
		return
	}
	m.pendingLeaderTransfers[shardID] = epoch
}
