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
