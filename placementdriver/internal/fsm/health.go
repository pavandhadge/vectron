// health.go - Worker health monitoring and shard management
//
// This file adds critical distributed system features:
// - Worker health checking with configurable timeouts
// - Automatic shard failover from unhealthy workers
// - Dead worker cleanup
// - Shard rebalancing

package fsm

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Constants for health monitoring
const (
	// WorkerTimeout is the maximum time since last heartbeat before a worker is considered unhealthy
	WorkerTimeout = 30 * time.Second

	// WorkerCleanupTimeout is the time after which a dead worker is removed from the system
	WorkerCleanupTimeout = 24 * time.Hour

	// DefaultReplicationFactor is the default number of replicas for each shard
	DefaultReplicationFactor = 3

	// MinReplicationFactor is the minimum allowed replication factor
	MinReplicationFactor = 1

	// MaxReplicationFactor is the maximum allowed replication factor
	MaxReplicationFactor = 5

	// HotShardThresholdQPS is the query rate threshold for detecting hot shards
	HotShardThresholdQPS = 1000.0 // 1000 QPS

	// HotShardThresholdLatencyMs is the latency threshold for detecting hot shards
	HotShardThresholdLatencyMs = 100.0 // 100ms
)

// ReplicationFactor returns the configured replication factor (clamped).
func ReplicationFactor() int {
	val := strings.TrimSpace(os.Getenv("VECTRON_REPLICATION_FACTOR"))
	if val == "" {
		return DefaultReplicationFactor
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return DefaultReplicationFactor
	}
	if n < MinReplicationFactor {
		return MinReplicationFactor
	}
	if n > MaxReplicationFactor {
		return MaxReplicationFactor
	}
	return n
}

// IsWorkerHealthy checks if a worker is healthy based on its last heartbeat
func (f *FSM) IsWorkerHealthy(workerID uint64) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	worker, ok := f.Workers[workerID]
	if !ok {
		return false
	}

	if worker.State != WorkerStateReady {
		return false
	}
	return time.Since(worker.LastHeartbeat) < WorkerTimeout
}

// GetHealthyWorkers returns a list of all healthy workers
func (f *FSM) GetHealthyWorkers() []WorkerInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var healthy []WorkerInfo
	for _, worker := range f.Workers {
		if worker.State == WorkerStateReady && time.Since(worker.LastHeartbeat) < WorkerTimeout {
			healthy = append(healthy, worker)
		}
	}
	return healthy
}

// GetDrainingWorkers returns workers marked as draining.
func (f *FSM) GetDrainingWorkers() []WorkerInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var draining []WorkerInfo
	for _, worker := range f.Workers {
		if worker.State == WorkerStateDraining {
			draining = append(draining, worker)
		}
	}
	return draining
}

// GetUnhealthyWorkers returns a list of workers that have timed out
func (f *FSM) GetUnhealthyWorkers() []WorkerInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var unhealthy []WorkerInfo
	for _, worker := range f.Workers {
		if time.Since(worker.LastHeartbeat) >= WorkerTimeout {
			unhealthy = append(unhealthy, worker)
		}
	}
	return unhealthy
}

// GetDeadWorkers returns workers that have been dead for longer than WorkerCleanupTimeout
func (f *FSM) GetDeadWorkers() []WorkerInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var dead []WorkerInfo
	for _, worker := range f.Workers {
		if time.Since(worker.LastHeartbeat) >= WorkerCleanupTimeout {
			dead = append(dead, worker)
		}
	}
	return dead
}

// GetShardReplicas returns the list of replica worker IDs for a shard
func (f *FSM) GetShardReplicas(shardID uint64) []uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	for _, collection := range f.Collections {
		if shard, ok := collection.Shards[shardID]; ok {
			return shard.Replicas
		}
	}
	return nil
}

// GetShardsOnWorker returns all shards that have a replica on the given worker
func (f *FSM) GetShardsOnWorker(workerID uint64) []*ShardInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var shards []*ShardInfo
	for _, collection := range f.Collections {
		for _, shard := range collection.Shards {
			for _, replicaID := range shard.Replicas {
				if replicaID == workerID {
					shards = append(shards, shard)
					break
				}
			}
		}
	}
	return shards
}

// IsShardRunningOnWorker checks if a worker reports a shard as running.
func (f *FSM) IsShardRunningOnWorker(workerID uint64, shardID uint64) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	worker, ok := f.Workers[workerID]
	if !ok {
		return false
	}
	for _, runningID := range worker.RunningShards {
		if runningID == shardID {
			return true
		}
	}
	return false
}

// GetUnderReplicatedShards returns shards that have fewer healthy replicas than required
func (f *FSM) GetUnderReplicatedShards() []*ShardInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var underReplicated []*ShardInfo
	for _, collection := range f.Collections {
		for _, shard := range collection.Shards {
			healthyCount := 0
			for _, replicaID := range shard.Replicas {
				if worker, ok := f.Workers[replicaID]; ok {
					if worker.State == WorkerStateReady && time.Since(worker.LastHeartbeat) < WorkerTimeout {
						healthyCount++
					}
				}
			}

			// Consider under-replicated if below the default replication factor
			targetReplicas := ReplicationFactor()

			if healthyCount < targetReplicas {
				underReplicated = append(underReplicated, shard)
			}
		}
	}
	return underReplicated
}

// CountHealthyReplicas returns the number of healthy replicas for a shard
func (f *FSM) CountHealthyReplicas(shardID uint64) int {
	f.mu.RLock()
	defer f.mu.RUnlock()

	for _, collection := range f.Collections {
		if shard, ok := collection.Shards[shardID]; ok {
			healthyCount := 0
			for _, replicaID := range shard.Replicas {
				if worker, ok := f.Workers[replicaID]; ok {
					if worker.State == WorkerStateReady && time.Since(worker.LastHeartbeat) < WorkerTimeout {
						healthyCount++
					}
				}
			}
			return healthyCount
		}
	}
	return 0
}

// IsShardHealthy checks if a shard has at least one healthy replica
func (f *FSM) IsShardHealthy(shardID uint64) bool {
	return f.CountHealthyReplicas(shardID) > 0
}

// HotShardInfo provides details about a detected hot shard
type HotShardInfo struct {
	ShardID          uint64  `json:"shard_id"`
	Collection       string  `json:"collection"`
	QueriesPerSecond float64 `json:"queries_per_second"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	WorkerID         uint64  `json:"worker_id"` // Worker reporting this shard
	Reason           string  `json:"reason"`    // "high_qps", "high_latency"
}

// HealthReport provides a comprehensive health status of the cluster
type HealthReport struct {
	TotalWorkers          int
	HealthyWorkers        int
	UnhealthyWorkers      int
	TotalCollections      int
	TotalShards           int
	HealthyShards         int
	UnderReplicatedShards []*ShardInfo
	DeadWorkers           []WorkerInfo
	HotShards             []*HotShardInfo // Hot shards detected
	HotShardCount         int
}

// GetHealthReport generates a comprehensive health report
func (f *FSM) GetHealthReport() *HealthReport {
	f.mu.RLock()
	defer f.mu.RUnlock()

	report := &HealthReport{
		TotalWorkers:     len(f.Workers),
		HealthyWorkers:   0,
		UnhealthyWorkers: 0,
		TotalCollections: len(f.Collections),
		TotalShards:      0,
		HealthyShards:    0,
	}

	// Count worker health
	for _, worker := range f.Workers {
		if worker.State == WorkerStateReady && time.Since(worker.LastHeartbeat) < WorkerTimeout {
			report.HealthyWorkers++
		} else {
			report.UnhealthyWorkers++
		}

		if time.Since(worker.LastHeartbeat) >= WorkerCleanupTimeout {
			report.DeadWorkers = append(report.DeadWorkers, worker)
		}
	}

	// Check shard health
	for _, collection := range f.Collections {
		for _, shard := range collection.Shards {
			report.TotalShards++

			healthyCount := 0
			for _, replicaID := range shard.Replicas {
				if worker, ok := f.Workers[replicaID]; ok {
					if worker.State == WorkerStateReady && time.Since(worker.LastHeartbeat) < WorkerTimeout {
						healthyCount++
					}
				}
			}

			if healthyCount > 0 {
				report.HealthyShards++
			}

			targetReplicas := ReplicationFactor()

			if healthyCount < targetReplicas {
				report.UnderReplicatedShards = append(report.UnderReplicatedShards, shard)
			}

			// Check for hot shard
			isHot := false
			reason := ""
			if shard.QueriesPerSecond > HotShardThresholdQPS {
				isHot = true
				reason = "high_qps"
			} else if shard.AvgLatencyMs > HotShardThresholdLatencyMs {
				isHot = true
				reason = "high_latency"
			}

			if isHot {
				report.HotShards = append(report.HotShards, &HotShardInfo{
					ShardID:          shard.ShardID,
					Collection:       shard.Collection,
					QueriesPerSecond: shard.QueriesPerSecond,
					AvgLatencyMs:     shard.AvgLatencyMs,
					WorkerID:         shard.LeaderID, // Use leader as the worker
					Reason:           reason,
				})
			}
		}
	}

	report.HotShardCount = len(report.HotShards)

	return report
}

// PrintHealthReport prints a formatted health report
func (r *HealthReport) PrintHealthReport() {
	fmt.Println("\n=== Cluster Health Report ===")
	fmt.Printf("Workers: %d total, %d healthy, %d unhealthy\n",
		r.TotalWorkers, r.HealthyWorkers, r.UnhealthyWorkers)
	fmt.Printf("Collections: %d\n", r.TotalCollections)
	fmt.Printf("Shards: %d total, %d healthy\n", r.TotalShards, r.HealthyShards)

	if len(r.UnderReplicatedShards) > 0 {
		fmt.Printf("⚠️  Under-replicated shards: %d\n", len(r.UnderReplicatedShards))
		for _, shard := range r.UnderReplicatedShards {
			fmt.Printf("   - Shard %d (collection: %s)\n", shard.ShardID, shard.Collection)
		}
	}

	if len(r.DeadWorkers) > 0 {
		fmt.Printf("☠️  Dead workers (can be cleaned up): %d\n", len(r.DeadWorkers))
		for _, worker := range r.DeadWorkers {
			fmt.Printf("   - Worker %d (last seen: %s)\n",
				worker.ID, time.Since(worker.LastHeartbeat).Round(time.Minute))
		}
	}

	if len(r.HotShards) > 0 {
		fmt.Printf("🔥 Hot shards detected: %d\n", len(r.HotShards))
		for _, shard := range r.HotShards {
			fmt.Printf("   - Shard %d (collection: %s): %.1f QPS, %.1fms latency [%s]\n",
				shard.ShardID, shard.Collection, shard.QueriesPerSecond, shard.AvgLatencyMs, shard.Reason)
		}
	}

	if len(r.UnderReplicatedShards) == 0 && len(r.DeadWorkers) == 0 && len(r.HotShards) == 0 {
		fmt.Println("✅ Cluster is healthy!")
	}
	fmt.Println("=============================")
}
