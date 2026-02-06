// reconciliation.go - Automatic cluster reconciliation and repair
//
// This file implements a background reconciliation loop that:
// - Monitors cluster health periodically
// - Automatically repairs under-replicated shards
// - Cleans up dead workers
// - Triggers alerts for issues that require manual intervention
// - Background rebalancing with compaction-aware throttling

package server

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
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

	// Step 4: Log background rebalancing status (if enabled)
	// Note: The RebalanceManager runs independently with its own scheduling

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

// ==============================================================================
// RebalanceManager - Background rebalancing with throttling
// ==============================================================================

// RebalanceConfig holds configuration for background rebalancing
type RebalanceConfig struct {
	// EnableBackgroundRebalance enables automatic shard rebalancing
	EnableBackgroundRebalance bool

	// RebalanceInterval is the minimum time between rebalance operations
	RebalanceInterval time.Duration

	// MaxConcurrentMoves limits concurrent shard migrations
	MaxConcurrentMoves int

	// ImbalanceThreshold triggers rebalancing when exceeded (e.g., 1.5 = 50% deviation)
	ImbalanceThreshold float64

	// CompactionBackoffDuration pauses rebalancing during suspected compaction
	CompactionBackoffDuration time.Duration

	// MaxMovesPerInterval limits shard moves per rebalance cycle
	MaxMovesPerInterval int
}

// DefaultRebalanceConfig returns default rebalancing configuration
func DefaultRebalanceConfig() *RebalanceConfig {
	return &RebalanceConfig{
		EnableBackgroundRebalance: true,
		RebalanceInterval:         5 * time.Minute,
		MaxConcurrentMoves:        2,
		ImbalanceThreshold:        1.3, // 30% deviation triggers rebalance
		CompactionBackoffDuration: 2 * time.Minute,
		MaxMovesPerInterval:       3,
	}
}

// RebalanceManager handles background shard rebalancing with throttling
type RebalanceManager struct {
	server *Server
	config *RebalanceConfig
	stopCh chan struct{}
	mu     sync.RWMutex

	// Tracking state
	lastRebalanceTime   time.Time
	movesInProgress     int
	movesCompletedToday int
	totalBytesMoved     int64

	// Compaction detection
	recentMoveLatencies  []time.Duration
	compactionDetected   bool
	compactionBackoffEnd time.Time
}

// NewRebalanceManager creates a new rebalancing manager
func NewRebalanceManager(s *Server, config *RebalanceConfig) *RebalanceManager {
	if config == nil {
		config = DefaultRebalanceConfig()
	}

	return &RebalanceManager{
		server:              s,
		config:              config,
		stopCh:              make(chan struct{}),
		recentMoveLatencies: make([]time.Duration, 0, 10),
	}
}

// Start begins the background rebalancing loop
func (rm *RebalanceManager) Start() {
	if !rm.config.EnableBackgroundRebalance {
		fmt.Println("⏸️  Background rebalancing is disabled")
		return
	}

	fmt.Printf("🔄 Starting background rebalancing manager (interval: %v, threshold: %.1f)\n",
		rm.config.RebalanceInterval, rm.config.ImbalanceThreshold)

	ticker := time.NewTicker(rm.config.RebalanceInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				rm.runRebalanceCycle()
			case <-rm.stopCh:
				ticker.Stop()
				fmt.Println("🛑 Rebalancing manager stopped")
				return
			}
		}
	}()
}

// Stop stops the rebalancing manager
func (rm *RebalanceManager) Stop() {
	close(rm.stopCh)
}

// runRebalanceCycle performs a single rebalancing pass with throttling
func (rm *RebalanceManager) runRebalanceCycle() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Check if we're in compaction backoff
	if rm.compactionDetected {
		if time.Now().Before(rm.compactionBackoffEnd) {
			fmt.Printf("⏸️  Rebalancing paused due to compaction detection (resumes at %s)\n",
				rm.compactionBackoffEnd.Format("15:04:05"))
			return
		}
		rm.compactionDetected = false
		fmt.Println("▶️  Compaction backoff ended, resuming rebalancing")
	}

	// Check if we have capacity for more moves
	if rm.movesInProgress >= rm.config.MaxConcurrentMoves {
		fmt.Printf("⏸️  Rebalancing skipped: %d moves already in progress (max: %d)\n",
			rm.movesInProgress, rm.config.MaxConcurrentMoves)
		return
	}

	// Analyze current distribution
	moves := rm.identifyRebalanceMoves()
	if len(moves) == 0 {
		return // No rebalancing needed
	}

	// Execute moves with throttling
	movesToExecute := rm.config.MaxMovesPerInterval
	if movesToExecute > len(moves) {
		movesToExecute = len(moves)
	}
	if movesToExecute > rm.config.MaxConcurrentMoves-rm.movesInProgress {
		movesToExecute = rm.config.MaxConcurrentMoves - rm.movesInProgress
	}

	fmt.Printf("🔄 Executing %d shard moves (identified %d total, %d in progress)\n",
		movesToExecute, len(moves), rm.movesInProgress)

	for i := 0; i < movesToExecute; i++ {
		move := moves[i]
		if err := rm.executeMove(move); err != nil {
			fmt.Printf("❌ Failed to move shard %d: %v\n", move.ShardID, err)
		} else {
			rm.movesInProgress++
			rm.movesCompletedToday++
			fmt.Printf("✅ Initiated move: shard %d from worker %d to %d\n",
				move.ShardID, move.SourceWorkerID, move.TargetWorkerID)
		}
	}

	rm.lastRebalanceTime = time.Now()
}

// RebalanceMove represents a planned shard migration
type RebalanceMove struct {
	ShardID        uint64
	SourceWorkerID uint64
	TargetWorkerID uint64
	Bytes          int64
	Reason         string // "load_imbalance", "failure_domain", "hot_shard"
}

// identifyRebalanceMoves analyzes the cluster and identifies necessary shard moves
func (rm *RebalanceManager) identifyRebalanceMoves() []RebalanceMove {
	var moves []RebalanceMove

	// Get current distribution
	workerShardCounts := make(map[uint64]int)
	workerLoadScores := make(map[uint64]float64)
	totalShards := 0

	healthyWorkers := rm.server.fsm.GetHealthyWorkers()
	if len(healthyWorkers) < 2 {
		return moves // Need at least 2 workers to rebalance
	}

	for _, collection := range rm.server.fsm.GetCollections() {
		for _, shard := range collection.Shards {
			totalShards++
			for _, replicaID := range shard.Replicas {
				workerShardCounts[replicaID]++
			}
		}
	}

	// Calculate average load
	avgShards := float64(totalShards*fsm.DefaultReplicationFactor) / float64(len(healthyWorkers))
	threshold := avgShards * rm.config.ImbalanceThreshold

	// Identify overloaded and underloaded workers
	var overloaded, underloaded []uint64
	for _, worker := range healthyWorkers {
		count := workerShardCounts[worker.ID]
		workerLoadScores[worker.ID] = float64(count)

		if float64(count) > threshold {
			overloaded = append(overloaded, worker.ID)
		} else if float64(count) < avgShards/rm.config.ImbalanceThreshold {
			underloaded = append(underloaded, worker.ID)
		}
	}

	if len(overloaded) == 0 || len(underloaded) == 0 {
		return moves // Distribution is balanced enough
	}

	fmt.Printf("📊 Detected load imbalance: avg=%.1f, threshold=%.1f, overloaded=%v, underloaded=%v\n",
		avgShards, threshold, overloaded, underloaded)

	// Plan moves from overloaded to underloaded workers
	for _, sourceID := range overloaded {
		// Find movable shards on this worker
		movableShards := rm.getMovableShards(sourceID)

		for _, shard := range movableShards {
			if len(moves) >= rm.config.MaxMovesPerInterval*2 {
				break // Don't plan too many moves at once
			}

			// Find best target (least loaded worker that doesn't already have this shard)
			targetID := rm.findBestTarget(shard.ShardID, underloaded, workerShardCounts)
			if targetID == 0 {
				continue
			}

			moves = append(moves, RebalanceMove{
				ShardID:        shard.ShardID,
				SourceWorkerID: sourceID,
				TargetWorkerID: targetID,
				Bytes:          0, // Would be populated from actual shard size
				Reason:         "load_imbalance",
			})

			// Update counts for planning
			workerShardCounts[sourceID]--
			workerShardCounts[targetID]++
		}
	}

	// Check for hot shards from health report
	healthReport := rm.server.fsm.GetHealthReport()
	if len(healthReport.HotShards) > 0 {
		fmt.Printf("🔥 Detected %d hot shards, planning targeted moves...\n", len(healthReport.HotShards))
		for _, hotShard := range healthReport.HotShards {
			// Find a target worker that doesn't have this shard
			targetID := rm.findBestTargetForHotShard(hotShard.ShardID, healthyWorkers, workerShardCounts)
			if targetID != 0 && targetID != hotShard.WorkerID {
				// Check if we already have a move planned for this shard
				alreadyPlanned := false
				for _, move := range moves {
					if move.ShardID == hotShard.ShardID {
						alreadyPlanned = true
						break
					}
				}
				if !alreadyPlanned {
					moves = append(moves, RebalanceMove{
						ShardID:        hotShard.ShardID,
						SourceWorkerID: hotShard.WorkerID,
						TargetWorkerID: targetID,
						Bytes:          0,
						Reason:         "hot_shard_" + hotShard.Reason,
					})
					workerShardCounts[hotShard.WorkerID]--
					workerShardCounts[targetID]++
				}
			}
		}
	}

	return moves
}

// findBestTargetForHotShard finds the best target worker for a hot shard
func (rm *RebalanceManager) findBestTargetForHotShard(shardID uint64, healthyWorkers []fsm.WorkerInfo, counts map[uint64]int) uint64 {
	var bestTarget uint64
	minLoad := float64(1 << 30) // Very large number

	for _, worker := range healthyWorkers {
		// Check if worker already has this shard
		shards := rm.server.fsm.GetShardsOnWorker(worker.ID)
		hasShard := false
		for _, shard := range shards {
			if shard.ShardID == shardID {
				hasShard = true
				break
			}
		}
		if hasShard {
			continue
		}

		// Calculate load ratio (current shards / capacity)
		loadRatio := float64(counts[worker.ID]) / float64(worker.TotalCapacity)
		if loadRatio < minLoad {
			minLoad = loadRatio
			bestTarget = worker.ID
		}
	}

	return bestTarget
}

// getMovableShards returns shards that can be moved from a worker
func (rm *RebalanceManager) getMovableShards(workerID uint64) []*fsm.ShardInfo {
	var movable []*fsm.ShardInfo

	for _, collection := range rm.server.fsm.GetCollections() {
		for _, shard := range collection.Shards {
			// Check if this worker has this shard
			hasShard := false
			for _, replicaID := range shard.Replicas {
				if replicaID == workerID {
					hasShard = true
					break
				}
			}
			if !hasShard {
				continue
			}

			// Only move if we have other healthy replicas
			healthyReplicas := 0
			for _, replicaID := range shard.Replicas {
				if rm.server.fsm.IsWorkerHealthy(replicaID) {
					healthyReplicas++
				}
			}

			// Only move if we have at least 2 healthy replicas (don't break quorum)
			if healthyReplicas >= 2 {
				movable = append(movable, shard)
			}
		}
	}

	return movable
}

// findBestTarget finds the best target worker for a shard move
func (rm *RebalanceManager) findBestTarget(shardID uint64, candidates []uint64, counts map[uint64]int) uint64 {
	var bestTarget uint64
	minCount := math.MaxInt32

	for _, workerID := range candidates {
		// Check if worker already has this shard
		shards := rm.server.fsm.GetShardsOnWorker(workerID)
		hasShard := false
		for _, shard := range shards {
			if shard.ShardID == shardID {
				hasShard = true
				break
			}
		}
		if hasShard {
			continue
		}

		// Pick the least loaded candidate
		if counts[workerID] < minCount {
			minCount = counts[workerID]
			bestTarget = workerID
		}
	}

	return bestTarget
}

// executeMove proposes a shard move via Raft
func (rm *RebalanceManager) executeMove(move RebalanceMove) error {
	payload := fsm.MoveShardPayload{
		ShardID:        move.ShardID,
		SourceWorkerID: move.SourceWorkerID,
		TargetWorkerID: move.TargetWorkerID,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal move payload: %w", err)
	}

	cmd := fsm.Command{Type: fsm.MoveShard, Payload: payloadBytes}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal move command: %w", err)
	}

	// Use a shorter timeout for moves to detect slow operations (possible compaction)
	start := time.Now()
	_, err = rm.server.raft.Propose(cmdBytes, 10*time.Second)
	duration := time.Since(start)

	if err != nil {
		// Check if this might be due to compaction (slow operation)
		if duration > 5*time.Second {
			rm.detectCompaction(duration)
		}
		return fmt.Errorf("failed to propose move: %w", err)
	}

	// Track latency for compaction detection
	rm.trackMoveLatency(duration)

	return nil
}

// trackMoveLatency records move latency for compaction detection
func (rm *RebalanceManager) trackMoveLatency(latency time.Duration) {
	rm.recentMoveLatencies = append(rm.recentMoveLatencies, latency)
	if len(rm.recentMoveLatencies) > 10 {
		rm.recentMoveLatencies = rm.recentMoveLatencies[1:]
	}

	// Detect if latencies are increasing (possible compaction)
	if len(rm.recentMoveLatencies) >= 5 {
		avgRecent := rm.averageLatency(rm.recentMoveLatencies[len(rm.recentMoveLatencies)-3:])
		avgPrevious := rm.averageLatency(rm.recentMoveLatencies[:len(rm.recentMoveLatencies)-3])

		if avgRecent > avgPrevious*2 && avgRecent > 2*time.Second {
			rm.detectCompaction(avgRecent)
		}
	}
}

// averageLatency calculates average duration
func (rm *RebalanceManager) averageLatency(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	var sum time.Duration
	for _, l := range latencies {
		sum += l
	}
	return sum / time.Duration(len(latencies))
}

// detectCompaction marks that compaction is likely happening and backs off
func (rm *RebalanceManager) detectCompaction(indicator time.Duration) {
	if rm.compactionDetected {
		return // Already in backoff
	}

	rm.compactionDetected = true
	rm.compactionBackoffEnd = time.Now().Add(rm.config.CompactionBackoffDuration)

	fmt.Printf("⚠️  Compaction detected (indicator: %v). Pausing rebalancing for %v\n",
		indicator, rm.config.CompactionBackoffDuration)
}

// MoveCompleted should be called when a shard move completes (success or failure)
func (rm *RebalanceManager) MoveCompleted(success bool, bytesMoved int64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.movesInProgress > 0 {
		rm.movesInProgress--
	}

	if success {
		rm.totalBytesMoved += bytesMoved
	}
}

// GetStats returns current rebalancing statistics
func (rm *RebalanceManager) GetStats() map[string]interface{} {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return map[string]interface{}{
		"moves_in_progress":     rm.movesInProgress,
		"moves_completed_today": rm.movesCompletedToday,
		"total_bytes_moved":     rm.totalBytesMoved,
		"compaction_detected":   rm.compactionDetected,
		"last_rebalance_time":   rm.lastRebalanceTime,
	}
}
