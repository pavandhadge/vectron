package searchonly

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/pavandhadge/vectron/shared/proto/worker"
	"github.com/pavandhadge/vectron/worker/internal/idxhnsw"
	"github.com/pavandhadge/vectron/worker/internal/pd"
	"github.com/pavandhadge/vectron/worker/internal/shard"
	"github.com/pavandhadge/vectron/worker/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type localSearcher struct {
	mu    sync.RWMutex
	index *idxhnsw.HNSW
	cfg   storage.HNSWConfig
	dim   int
}

func newLocalSearcher(info *pd.ShardInfo) *localSearcher {
	durability := shard.ParseDurabilityProfile()
	writeSpeedMode := false
	cfg := shard.DefaultHNSWConfig(int(info.Dimension), info.Distance, durability, writeSpeedMode)
	return &localSearcher{
		cfg: cfg,
		dim: int(info.Dimension),
	}
}

func (s *localSearcher) loadSnapshot(data []byte) error {
	store := idxhnsw.NewMemStore()
	h := idxhnsw.NewHNSW(store, s.dim, toIndexConfig(s.cfg))
	if len(data) > 0 {
		if err := h.Load(bytes.NewReader(data)); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.index = h
	s.mu.Unlock()
	return nil
}

func (s *localSearcher) applyUpdate(upd storage.WALUpdate) {
	s.mu.RLock()
	h := s.index
	s.mu.RUnlock()
	if h == nil {
		return
	}
	if upd.Delete {
		_ = h.Delete(upd.ID)
		return
	}
	vec, _, err := storage.DecodeVectorWithMeta(upd.Value)
	if err != nil || vec == nil {
		return
	}
	_ = h.Add(upd.ID, vec)
}

func (s *localSearcher) Search(query shard.SearchQuery) (*shard.SearchResult, error) {
	s.mu.RLock()
	h := s.index
	cfg := s.cfg
	s.mu.RUnlock()
	if h == nil {
		return nil, shard.ErrShardNotReady
	}
	k := query.K
	if k <= 0 {
		k = 10
	}
	ef := cfg.EfSearch
	if k > 0 {
		adaptive := k * 2
		if adaptive < k {
			adaptive = k
		}
		if adaptive < ef {
			ef = adaptive
		}
	}
	if cfg.AdaptiveEfEnabled {
		mult := cfg.AdaptiveEfMultiplier
		if mult <= 0 {
			mult = 2
		}
		adaptive := k * mult
		minEf := cfg.AdaptiveEfMin
		if minEf <= 0 {
			minEf = k
		}
		maxEf := cfg.AdaptiveEfMax
		if maxEf <= 0 {
			maxEf = cfg.EfSearch
		}
		if adaptive < minEf {
			adaptive = minEf
		}
		if adaptive > maxEf {
			adaptive = maxEf
		}
		if adaptive > ef {
			ef = adaptive
		}
	}
	if cfg.AdaptiveEfDimScale > 0 && cfg.AdaptiveEfDimScale < 1 {
		scaled := int(float64(ef) * cfg.AdaptiveEfDimScale)
		if scaled < k {
			scaled = k
		}
		if scaled > 0 && scaled < ef {
			ef = scaled
		}
	}
	if cfg.MultiStageEnabled && (!cfg.QuantizeVectors || cfg.QuantizeKeepFloatVectors) {
		stage1Ef := cfg.Stage1Ef
		if stage1Ef <= 0 {
			stage1Ef = ef / 2
		}
		candidateFactor := cfg.Stage1CandidateFactor
		if candidateFactor <= 0 {
			candidateFactor = 4
		}
		ids, scores := h.SearchTwoStage(query.Vector, k, stage1Ef, candidateFactor)
		return &shard.SearchResult{IDs: ids, Scores: scores}, nil
	}
	ids, scores := h.SearchWithEf(query.Vector, k, ef)
	return &shard.SearchResult{IDs: ids, Scores: scores}, nil
}

func toIndexConfig(cfg storage.HNSWConfig) idxhnsw.HNSWConfig {
	return idxhnsw.HNSWConfig{
		M:                 cfg.M,
		EfConstruction:    cfg.EfConstruction,
		EfSearch:          cfg.EfSearch,
		Distance:          cfg.DistanceMetric,
		PersistNodes:      cfg.PersistNodes,
		EnableNorms:       cfg.EnableNorms,
		NormalizeVectors:  cfg.NormalizeVectors,
		QuantizeVectors:   cfg.QuantizeVectors,
		KeepFloatVectors:  cfg.QuantizeKeepFloatVectors,
		SearchParallelism: cfg.SearchParallelism,
		PruneEnabled:      cfg.PruneEnabled,
		PruneMaxNodes:     cfg.PruneMaxNodes,
	}
}

func (r *Replica) replicateFrom(addr string) error {
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	client := worker.NewWorkerServiceClient(conn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Snapshot stream
	snapStream, err := client.StreamHNSWSnapshot(ctx, &worker.HNSWSnapshotRequest{ShardId: r.shardID})
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	var snapshotTS int64
	for {
		chunk, err := snapStream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		if chunk == nil {
			continue
		}
		if chunk.SnapshotTsUnixNano > 0 {
			snapshotTS = chunk.SnapshotTsUnixNano
		}
		if len(chunk.Data) > 0 {
			buf.Write(chunk.Data)
		}
		if chunk.Done {
			break
		}
	}
	if err := r.searcher.loadSnapshot(buf.Bytes()); err != nil {
		return err
	}
	r.ready.Store(true)

	// Updates stream
	updStream, err := client.StreamHNSWUpdates(ctx, &worker.HNSWUpdatesRequest{
		ShardId:        r.shardID,
		FromTsUnixNano: snapshotTS,
	})
	if err != nil {
		return err
	}

	for {
		select {
		case <-r.stopCh:
			return nil
		default:
		}
		batch, err := updStream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		for _, upd := range batch.Updates {
			r.searcher.applyUpdate(storage.WALUpdate{
				ID:     upd.Id,
				Value:  upd.Value,
				Delete: upd.Delete,
				TS:     upd.TsUnixNano,
			})
		}
	}
}
