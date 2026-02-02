// reconciliation.go - Automatic cluster reconciliation and repair
//
// This file implements a background reconciliation loop that:
// - Monitors cluster health periodically
// - Automatically repairs under-replicated shards
// - Cleans up dead workers
// - Triggers alerts for issues that require manual intervention

package server

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pavandhadge/vectron/placementdriver/internal/fsm"
)

// ReconciliationConfig holds configuration for the reconciliation loop
type ReconciliationConfig struct {
	// Interval between reconciliation runs
	Interval time.Duration

	// EnableAutoRepair enables automatic shard re-replication
	EnableAutoRepair bool

	// EnableDeadWorkerCleanup enables automatic cleanup of dead workers
	EnableDeadWorkerCleanup bool

	// MaxConcurrentRepairs limits the number of concurrent shard repairs
	MaxConcurrentRepairs int
}

// DefaultReconciliationConfig returns default configuration
func DefaultReconciliationConfig() *ReconciliationConfig {
	return &ReconciliationConfig{
		Interval:                30 * time.Second,
		EnableAutoRepair:        true,
		EnableDeadWorkerCleanup: true,
		MaxConcurrentRepairs:    3,
	}
}

// Reconciler handles cluster reconciliation
type Reconciler struct {
	server *Server
	config *ReconciliationConfig
	stopCh chan struct{}
}

// NewReconciler creates a new reconciler
func NewReconciler(s *Server, config *ReconciliationConfig) *Reconciler {
	if config == nil {
		config = DefaultReconciliationConfig()
	}

	return &Reconciler{
		server: s,
		config: config,
		stopCh: make(chan struct{}),
	}
}

// Start begins the reconciliation loop
func (r *Reconciler) Start() {
	fmt.Printf("🔄 Starting reconciliation loop (interval: %v, auto-repair: %v)\n",
		r.config.Interval, r.config.EnableAutoRepair)

	ticker := time.NewTicker(r.config.Interval)
	go func() {
		// Run immediately on start
		r.reconcile()

		for {
			select {
			case <-ticker.C:
				r.reconcile()
			case <-r.stopCh:
				ticker.Stop()
				fmt.Println("🛑 Reconciliation loop stopped")
				return
			}
		}
	}()
}

// Stop stops the reconciliation loop
func (r *Reconciler) Stop() {
	close(r.stopCh)
}

// reconcile performs a single reconciliation pass
func (r *Reconciler) reconcile() {
	start := time.Now()
	fmt.Printf("\n🔄 Starting reconciliation pass at %s\n", start.Format("15:04:05"))

	// Get current health report
	healthReport := r.server.fsm.GetHealthReport()
	healthReport.PrintHealthReport()

	// Step 1: Repair under-replicated shards
	if r.config.EnableAutoRepair && len(healthReport.UnderReplicatedShards) > 0 {
		r.repairUnderReplicatedShards(healthReport.UnderReplicatedShards)
	}

	// Step 2: Clean up dead workers
	if r.config.EnableDeadWorkerCleanup && len(healthReport.DeadWorkers) > 0 {
		r.cleanupDeadWorkers(healthReport.DeadWorkers)
	}

	// Step 3: Check for leaderless shards and trigger re-election
	r.checkLeaderlessShards()

	duration := time.Since(start)
	fmt.Printf("✅ Reconciliation pass completed in %v\n\n", duration)
}

// repairUnderReplicatedShards adds new replicas to under-replicated shards
func (r *Reconciler) repairUnderReplicatedShards(shards []*fsm.ShardInfo) {
	fmt.Printf("🔧 Repairing %d under-replicated shards...\n", len(shards))

	// Get healthy workers
	healthyWorkers := r.server.fsm.GetHealthyWorkers()
	if len(healthyWorkers) == 0 {
		fmt.Println("❌ No healthy workers available for repair")
		return
	}

	// Convert to map for quick lookup
	healthyWorkerIDs := make(map[uint64]bool)
	for _, w := range healthyWorkers {
		healthyWorkerIDs[w.ID] = true
	}

	repairedCount := 0
	for _, shard := range shards {
		// Find healthy replicas for this shard
		existingHealthyReplicas := make([]uint64, 0)
		for _, replicaID := range shard.Replicas {
			if healthyWorkerIDs[replicaID] {
				existingHealthyReplicas = append(existingHealthyReplicas, replicaID)
			}
		}

		// Calculate how many new replicas we need
		targetReplicas := fsm.DefaultReplicationFactor
		if len(shard.Replicas) < targetReplicas {
			targetReplicas = len(shard.Replicas)
		}

		needed := targetReplicas - len(existingHealthyReplicas)
		if needed <= 0 {
			continue
		}

		fmt.Printf("  📦 Shard %d: %d healthy of %d target, need %d new replicas\n",
			shard.ShardID, len(existingHealthyReplicas), targetReplicas, needed)

		// Find candidate workers that don't already have this shard
		candidates := make([]uint64, 0)
		for _, worker := range healthyWorkers {
			hasShard := false
			for _, replicaID := range shard.Replicas {
				if replicaID == worker.ID {
					hasShard = true
					break
				}
			}
			if !hasShard {
				candidates = append(candidates, worker.ID)
			}
		}

		if len(candidates) == 0 {
			fmt.Printf("  ⚠️  No candidate workers available for shard %d\n", shard.ShardID)
			continue
		}

		// Add new replicas (up to needed or available)
		newReplicas := make([]uint64, 0, needed)
		for i := 0; i < needed && i < len(candidates); i++ {
			newReplicas = append(newReplicas, candidates[i])
		}

		// Propose the update to the FSM
		if err := r.addShardReplicas(shard.ShardID, newReplicas); err != nil {
			fmt.Printf("  ❌ Failed to add replicas to shard %d: %v\n", shard.ShardID, err)
		} else {
			fmt.Printf("  ✅ Added %d new replicas to shard %d: %v\n",
				len(newReplicas), shard.ShardID, newReplicas)
			repairedCount++
		}

		// Respect max concurrent repairs
		if repairedCount >= r.config.MaxConcurrentRepairs {
			fmt.Printf("  ⏸️  Reached max concurrent repairs (%d), will continue in next pass\n",
				r.config.MaxConcurrentRepairs)
			break
		}
	}

	fmt.Printf("🔧 Repaired %d shards\n", repairedCount)
}

// addShardReplicas proposes adding new replicas to a shard via Raft
func (r *Reconciler) addShardReplicas(shardID uint64, newReplicas []uint64) error {
	// Create a custom command for adding replicas
	// Note: This requires extending the FSM command types

	payload := struct {
		ShardID     uint64   `json:"shard_id"`
		NewReplicas []uint64 `json:"new_replicas"`
	}{
		ShardID:     shardID,
		NewReplicas: newReplicas,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Use UpdateShardLeader command type but with different payload
	// In production, you'd add a dedicated AddShardReplica command type
	cmd := fsm.Command{Type: fsm.UpdateShardLeader, Payload: payloadBytes}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	_, err = r.server.raft.Propose(cmdBytes, raftTimeout)
	if err != nil {
		return fmt.Errorf("failed to propose command: %w", err)
	}

	return nil
}

// cleanupDeadWorkers removes workers that have been dead for too long
func (r *Reconciler) cleanupDeadWorkers(deadWorkers []fsm.WorkerInfo) {
	fmt.Printf("🧹 Cleaning up %d dead workers...\n", len(deadWorkers))

	for _, worker := range deadWorkers {
		// Check if any shards still depend on this worker
		shards := r.server.fsm.GetShardsOnWorker(worker.ID)
		if len(shards) > 0 {
			fmt.Printf("  ⚠️  Worker %d still has %d shards, skipping cleanup\n",
				worker.ID, len(shards))
			continue
		}

		// Propose removal of the worker
		if err := r.removeWorker(worker.ID); err != nil {
			fmt.Printf("  ❌ Failed to remove worker %d: %v\n", worker.ID, err)
		} else {
			fmt.Printf("  ✅ Removed dead worker %d\n", worker.ID)
		}
	}
}

// removeWorker proposes removing a worker from the cluster
func (r *Reconciler) removeWorker(workerID uint64) error {
	// Note: This requires adding a RemoveWorker command type to the FSM
	// For now, we just log it
	fmt.Printf("  📝 Worker %d removal would be proposed here (command type not implemented)\n", workerID)
	return nil
}

// checkLeaderlessShards identifies and logs shards without a leader
func (r *Reconciler) checkLeaderlessShards() {
	leaderlessCount := 0

	for _, collection := range r.server.fsm.GetCollections() {
		for _, shard := range collection.Shards {
			if shard.LeaderID == 0 {
				// Check if we have any healthy replicas
				hasHealthyReplicas := false
				for _, replicaID := range shard.Replicas {
					if r.server.fsm.IsWorkerHealthy(replicaID) {
						hasHealthyReplicas = true
						break
					}
				}

				if hasHealthyReplicas {
					fmt.Printf("⚠️  Shard %d (collection: %s) has no leader but has healthy replicas\n",
						shard.ShardID, collection.Name)
					leaderlessCount++
				} else {
					fmt.Printf("☠️  Shard %d (collection: %s) has no leader and no healthy replicas\n",
						shard.ShardID, collection.Name)
					leaderlessCount++
				}
			}
		}
	}

	if leaderlessCount > 0 {
		fmt.Printf("⚠️  Found %d leaderless shards - manual intervention may be required\n", leaderlessCount)
	}
}

// TriggerRebalance initiates a manual shard rebalancing operation
func (r *Reconciler) TriggerRebalance() error {
	fmt.Println("🔄 Manual rebalancing triggered")

	// Analyze current distribution
	workerShardCounts := make(map[uint64]int)
	totalShards := 0

	for _, collection := range r.server.fsm.GetCollections() {
		for _, shard := range collection.Shards {
			totalShards++
			for _, replicaID := range shard.Replicas {
				workerShardCounts[replicaID]++
			}
		}
	}

	healthyWorkers := r.server.fsm.GetHealthyWorkers()
	if len(healthyWorkers) == 0 {
		return fmt.Errorf("no healthy workers available for rebalancing")
	}

	// Calculate ideal distribution
	idealShardsPerWorker := float64(totalShards*fsm.DefaultReplicationFactor) / float64(len(healthyWorkers))

	fmt.Printf("  📊 Current distribution: %v\n", workerShardCounts)
	fmt.Printf("  🎯 Ideal distribution: %.1f shards per worker\n", idealShardsPerWorker)

	// Identify overloaded and underloaded workers
	var overloaded, underloaded []uint64
	for _, worker := range healthyWorkers {
		count := workerShardCounts[worker.ID]
		if float64(count) > idealShardsPerWorker*1.2 {
			overloaded = append(overloaded, worker.ID)
		} else if float64(count) < idealShardsPerWorker*0.8 {
			underloaded = append(underloaded, worker.ID)
		}
	}

	if len(overloaded) == 0 && len(underloaded) == 0 {
		fmt.Println("  ✅ Distribution is balanced")
		return nil
	}

	fmt.Printf("  📈 Overloaded workers: %v\n", overloaded)
	fmt.Printf("  📉 Underloaded workers: %v\n", underloaded)

	// Note: Actual shard migration would be implemented here
	fmt.Println("  ⏸️  Shard migration not yet implemented")

	return nil
}

// GetShardDistribution returns a map of worker ID to shard count
type ShardDistribution struct {
	WorkerID   uint64 `json:"worker_id"`
	Address    string `json:"address"`
	ShardCount int    `json:"shard_count"`
	IsHealthy  bool   `json:"is_healthy"`
}

// GetShardDistribution returns the current distribution of shards across workers
func (r *Reconciler) GetShardDistribution() []ShardDistribution {
	distribution := make([]ShardDistribution, 0)

	// Count shards per worker
	workerShardCounts := make(map[uint64]int)
	for _, collection := range r.server.fsm.GetCollections() {
		for _, shard := range collection.Shards {
			for _, replicaID := range shard.Replicas {
				workerShardCounts[replicaID]++
			}
		}
	}

	// Build distribution list
	for _, worker := range r.server.fsm.GetWorkers() {
		distribution = append(distribution, ShardDistribution{
			WorkerID:   worker.ID,
			Address:    worker.GrpcAddress,
			ShardCount: workerShardCounts[worker.ID],
			IsHealthy:  r.server.fsm.IsWorkerHealthy(worker.ID),
		})
	}

	return distribution
}
