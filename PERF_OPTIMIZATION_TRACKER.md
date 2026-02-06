# Performance Optimization Tracker
Date: 2026-02-06
Context: Mixed load, heavy bulk writes + high search QPS (~1k search/sec per user).

## Pending
- Implement request coalescing batch search in gateway (deferred: approximate grouping can affect correctness; exact duplicates already handled by in-flight dedupe).
- Replace HNSW node map with slice/arena for better cache locality.

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
