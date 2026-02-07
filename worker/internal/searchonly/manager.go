package searchonly

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
	"os"
	"strings"

	"github.com/pavandhadge/vectron/worker/internal/pd"
	"github.com/pavandhadge/vectron/worker/internal/shard"
	placementpb "github.com/pavandhadge/vectron/shared/proto/placementdriver"
)

// Manager maintains local, read-only replicas of shards for search-only workers.
type Manager struct {
	pdClient *pd.Client

	mu              sync.RWMutex
	shards          map[uint64]*Replica
	shardCollections map[uint64]string
	shardEpochs     map[uint64]uint64
	allowedCollections map[string]struct{}
}

// NewManager creates a search-only shard manager.
func NewManager(pdClient *pd.Client) *Manager {
	allowed := parseCollectionAllowlist()
	return &Manager{
		pdClient:        pdClient,
		shards:          make(map[uint64]*Replica),
		shardCollections: make(map[uint64]string),
		shardEpochs:     make(map[uint64]uint64),
		allowedCollections: allowed,
	}
}

// SetPDClient injects the PD client after construction.
func (m *Manager) SetPDClient(client *pd.Client) {
	m.pdClient = client
}

// SyncShards updates local replicas based on PD assignments.
func (m *Manager) SyncShards(assignments []*pd.ShardAssignment) {
	desired := make(map[uint64]*pd.ShardAssignment)
	for _, assignment := range assignments {
		if assignment == nil || assignment.ShardInfo == nil {
			continue
		}
		if m.allowedCollections != nil {
			if _, ok := m.allowedCollections[assignment.ShardInfo.Collection]; !ok {
				continue
			}
		}
		desired[assignment.ShardInfo.ShardID] = assignment
	}

	m.mu.Lock()
	// Stop replicas that are no longer assigned.
	for shardID, rep := range m.shards {
		if _, ok := desired[shardID]; !ok {
			rep.Stop()
			delete(m.shards, shardID)
			delete(m.shardCollections, shardID)
			delete(m.shardEpochs, shardID)
		}
	}
	// Start or update replicas for desired shards.
	for shardID, assignment := range desired {
		m.shardEpochs[shardID] = assignment.ShardInfo.Epoch
		m.shardCollections[shardID] = assignment.ShardInfo.Collection
		existing := m.shards[shardID]
		if existing != nil {
			existing.UpdateLeader(assignment.ShardInfo.LeaderID)
			continue
		}
		rep := NewReplica(assignment, m.pdClient)
		m.shards[shardID] = rep
		rep.Start()
	}
	m.mu.Unlock()
}

// IsShardReady returns whether the shard has an initialized local index.
func (m *Manager) IsShardReady(shardID uint64) bool {
	m.mu.RLock()
	rep := m.shards[shardID]
	m.mu.RUnlock()
	if rep == nil {
		return false
	}
	return rep.IsReady()
}

// GetShards returns all shard IDs served by this worker.
func (m *Manager) GetShards() []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]uint64, 0, len(m.shards))
	for id, rep := range m.shards {
		if rep == nil || !rep.IsReady() {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// GetShardsForCollection returns shard IDs for the given collection.
func (m *Manager) GetShardsForCollection(collection string) []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]uint64, 0)
	for shardID, coll := range m.shardCollections {
		if coll != collection {
			continue
		}
		rep := m.shards[shardID]
		if rep == nil || !rep.IsReady() {
			continue
		}
		ids = append(ids, shardID)
	}
	return ids
}

// GetShardEpoch returns the current epoch for a shard.
func (m *Manager) GetShardEpoch(shardID uint64) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.shardEpochs[shardID]
}

// SearchShard executes a search against the local replica.
func (m *Manager) SearchShard(ctx context.Context, shardID uint64, query shard.SearchQuery, linearizable bool) (*shard.SearchResult, error) {
	if linearizable {
		return nil, shard.ErrLinearizableNotSupported
	}
	m.mu.RLock()
	rep := m.shards[shardID]
	m.mu.RUnlock()
	if rep == nil {
		return nil, shard.ErrShardNotReady
	}
	return rep.Search(query)
}

func parseCollectionAllowlist() map[string]struct{} {
	raw := strings.TrimSpace(os.Getenv("VECTRON_SEARCH_ONLY_COLLECTIONS"))
	if raw == "" {
		return nil
	}
	items := strings.Split(raw, ",")
	allow := make(map[string]struct{}, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		allow[name] = struct{}{}
	}
	if len(allow) == 0 {
		return nil
	}
	return allow
}

// --- PD ShardManager interface stubs ---

func (m *Manager) GetShardLeaderInfo() []*placementpb.ShardLeaderInfo { return nil }
func (m *Manager) GetShardMembershipInfo() []*placementpb.ShardMembershipInfo { return nil }
func (m *Manager) GetShardProgressInfo() []*placementpb.ShardProgressInfo { return nil }
func (m *Manager) GetLeaderTransferAcks() []*placementpb.ShardLeaderTransferAck { return nil }

// Replica holds local search state for a shard.
type Replica struct {
	assignment *pd.ShardAssignment
	pdClient   *pd.Client

	mu       sync.RWMutex
	ready    atomic.Bool
	shardID  uint64
	leaderID uint64

	stopCh chan struct{}

	searcher *localSearcher
}

func NewReplica(assignment *pd.ShardAssignment, pdClient *pd.Client) *Replica {
	return &Replica{
		assignment: assignment,
		pdClient:   pdClient,
		shardID:    assignment.ShardInfo.ShardID,
		leaderID:   assignment.ShardInfo.LeaderID,
		stopCh:     make(chan struct{}),
		searcher:   newLocalSearcher(assignment.ShardInfo),
	}
}

func (r *Replica) Start() {
	go r.run()
}

func (r *Replica) Stop() {
	close(r.stopCh)
}

func (r *Replica) UpdateLeader(leaderID uint64) {
	r.mu.Lock()
	r.leaderID = leaderID
	r.mu.Unlock()
}

func (r *Replica) IsReady() bool {
	return r.ready.Load()
}

func (r *Replica) Search(query shard.SearchQuery) (*shard.SearchResult, error) {
	return r.searcher.Search(query)
}

func (r *Replica) run() {
	backoff := 250 * time.Millisecond
	for {
		select {
		case <-r.stopCh:
			return
		default:
		}

		if r.pdClient == nil {
			time.Sleep(backoff)
			continue
		}
		leaderID := r.getLeaderID()
		if leaderID == 0 {
			time.Sleep(backoff)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		addr, err := r.pdClient.ResolveWorkerAddr(ctx, leaderID)
		cancel()
		if err != nil || addr == "" {
			time.Sleep(backoff)
			continue
		}

		if err := r.replicateFrom(addr); err != nil {
			log.Printf("search-only shard %d replicate error: %v", r.shardID, err)
			time.Sleep(backoff)
			if backoff < 5*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 250 * time.Millisecond
	}
}

func (r *Replica) getLeaderID() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.leaderID
}
