# Performance Optimization Tracker
Date: 2026-02-06
Context: Mixed load, heavy bulk writes + high search QPS (~1k search/sec per user).

## Pending
- Implement request coalescing batch search in gateway (deferred: approximate grouping can affect correctness; exact duplicates already handled by in-flight dedupe).

## Done
- Removed per-request stdout logging in gateway middleware to cut hot-path CPU.
- Precompiled API key name regex in auth middleware to avoid per-call compile.
- Added etcd secondary indexes for user-by-ID and apikeys-by-user to remove full scans (with legacy fallback).
- Parallelized worker BatchSearch with bounded concurrency and preserved response order.
- Reduced lock contention in gateway multi-worker search using per-worker local topK and merged once.
- Reduced lock contention in worker broadcast search using per-shard local topK and merged once.
- Optimized uncompressed vector serialization to avoid bytes.Buffer/binary.Write overhead.
- Fixed search result pooling correctness in gateway (removed pooled slices from returned/cached responses).
- Replaced HNSW visited map with marker array to reduce allocations.
- Switched auth rate limiter to fixed-window counters to avoid per-request slice scans.
- Replaced HNSW node map with slice storage for improved cache locality.
- Removed double normalization in storage search path (HNSW now normalizes once).
- Added pooled query normalization and pooled int8 quantization to cut search allocations.
- Enabled adaptive ef with dimension-based scaling for high-dimensional queries.
- Reduced per-insert normalization overhead by normalizing once.
- Made routing, worker list, and resolve cache TTLs configurable (defaults raised for throughput).
- Removed redundant normalization in bulk load path.
- Increased async indexing queue/batch sizes for higher write throughput.
- Avoided goroutine overhead for single-item batch search and single-shard broadcast search.
- Routed vector norm/normalization through SIMD dot product for AVX2/AVX-512 speedups.
- Added AVX2 quantization via cgo with safe fallback to scalar.
- Parallelized candidate filtering in HNSW search layer for large neighbor sets.
- Enabled quantized search with exact rerank by keeping float vectors (minimal quality loss).
- Raised Pebble defaults for write-heavy throughput (memtable, cache, L0 thresholds, compactions).
- Auto-configured GOMAXPROCS across services for full core utilization (with override).
- Search cache now uses TinyLFU admission for higher hit rates under hot query loads.
- Fanout search now requires shard routing to avoid querying multiple replicas (one replica per shard).
- Added market-traffic benchmark with hot queries + mixed read/write workload.
- Sharded in-flight search de-dup maps in gateway and worker to cut lock contention at high concurrency.
- Sharded search cache (TinyLFU) to reduce cache lock contention.
- Sharded worker connection pool to reduce global lock contention.
- Sharded worker resolve, worker list, and routing caches to reduce lock contention.
- Added singleflight-style routing and worker-list fetch to prevent thundering herd on PD.
- Pooled broadcast-search heaps and result slices in worker to cut allocations.
- Pooled neighbor node/id slices in HNSW search layer to reduce GC under concurrency.
- Reduced heap pressure in HNSW search via partial selection for large candidate sets.

---

## Optimization Backlog (Pending/Done Buttons)

### Pending
- [ ] Pool HNSW candidate/visited buffers and scratch slices to cut per-query allocations.
- [ ] Replace heap-based candidate management for small `ef` with bounded slices/partial select.
- [ ] Batch vector distance computations to reduce `cgocall` transitions.
- [ ] Reduce per-request syscall frequency by batching IO and avoiding small writes.
- [ ] Lower Pebble flush contention by coalescing flushes around high write load.
- [ ] Reduce lock contention in `StaleRead`/`Lookup` by minimizing shared state machine locks on read path.
- [ ] Add read-through cache for hot queries at worker layer (optional consistency tradeoff).
- [ ] Shard Pebble/state-machine instances by collection or partition to reduce lock hot spots.
- [ ] Tune HNSW `ef` dynamically by load (latency/recall tradeoff knob).
- [ ] Evaluate HNSW graph pruning settings to reduce search fanout under high load.
- [ ] Reduce gateway Redis round-trips or increase pool size to avoid lock contention.
- [ ] Add search-only and write-only benchmarks to isolate CPU vs. storage bottlenecks.
- [ ] Capture `heap` and `trace` profiles for allocation and scheduler root-causes.

### Done
- [x] Made Pebble background sync interval configurable and raised default to reduce flush contention.
- [x] Increased HNSW indexing flush interval default to coalesce more ops per batch.
- [x] Pooled distance/temp candidate slices in HNSW search to cut per-query allocations.
- [x] Removed duplicate distance calculation for the search start node.
- [x] Skip background Pebble flush when no writes occurred since last sync.
- [x] Reduced final-result sort cost by partial-selecting top K before sort.
- [x] Enabled HNSW hot index with conservative defaults to shorten read critical sections.
- [x] Added small-ef heap-free search path for HNSW to reduce heap overhead.
- [x] Added adaptive background sync backoff under sustained writes.
- [x] Added small worker-side search cache for non-linearizable queries.
- [x] Added optional HNSW neighbor cap (env-controlled) to limit neighbor expansion cost.
- [x] Coalesced WAL timestamps in batch writes to reduce per-item time.Now overhead.
- [x] Added optional search-key quantization for worker cache (env-controlled).
- [x] Expanded heap-free HNSW path to cover small ef up to 64 and reduced goroutine overhead in distance batches.
- [x] Added per-shard cache use inside worker search core to reduce StaleRead pressure (BatchSearch included).
- [x] Enabled WAL batch record for StoreVectorBatch with replay support.
- [x] Tuned hot index defaults (size up, cold ef scale down) to lower read cost.
- [x] Added cgo min-dimension threshold for SIMD dot products to reduce cgo overhead on small vectors.
- [x] Added durability profile flag to relax background sync + indexing flush cadence.
- [x] Added Redis pool size and min idle conns config for distributed cache.
- [x] Added batched int8 dot-product SIMD path (cgo) gated by env flags to reduce cgo transitions.
