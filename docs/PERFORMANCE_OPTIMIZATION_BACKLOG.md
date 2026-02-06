# Performance Optimization Backlog (Ranked by Impact)

This document ranks potential optimizations by expected impact on end‑to‑end performance. Items are grouped by tier and ordered roughly by benefit. Many are already implemented; they are included for completeness with a status marker.

Legend: [DONE] implemented, [PENDING] not implemented, [EXPERIMENT] requires validation

## Tier 0 — Highest Impact (order of priority)
1. [DONE] Reuse gRPC connections for worker fan‑out in API Gateway.
2. [DONE] Batch upsert per shard (single Raft proposal, single Pebble batch).
3. [DONE] Skip reranking when query text is empty.
4. [DONE] Pre‑merge/limit top‑K before reranker to avoid huge candidate sets.
5. [DONE] Non‑linearizable (stale) reads for search by default (configurable).
6. [DONE] Make reranking fully opt‑in (header/collection policy) to avoid unintended use.
7. [DONE] Enforce rerank RPC timeout + fallback (configurable).
8. [DONE] Lower HNSW search parallelization threshold for mid‑size searches.

## Tier 1 — Major Wins
1. [DONE] Remove per‑candidate Pebble existence checks in search.
2. [DONE] Replace JSON Raft commands with compact binary encoding (backward compatible).
3. [DONE] Reduce HNSW per‑query allocations using pools (vector memory pools).
4. [DONE] Cache vector norms for cosine distance to reduce CPU per distance calc.
5. [DONE] Reduce gRPC overhead with keepalive/window/buffer tuning across services.
6. [DONE] Fast path: avoid map creation for empty payloads end‑to‑end.
7. [DONE] gRPC response compression for large responses (evaluate CPU vs bandwidth).
8. [DONE] Connection pool size limits + circuit breaker for worker conns.
9. [PENDING] Vector prefetching for HNSW search to improve cache locality.
10. [PENDING] Adaptive search parameter tuning (dynamic EfSearch).
11. [PENDING] SIMD distance function optimizations (AVX‑512/NEON + runtime dispatch).
12. [PENDING] Pebble block cache optimization (hot/cold caches, tuned bloom filters).
13. [PENDING] HNSW graph pruning/edge optimization to reduce redundant edges.

## Tier 2 — Medium Wins
1. [DONE] Worker list cache in API Gateway (short TTL).
2. [DONE] Worker resolve cache in API Gateway (short TTL).
3. [DONE] Limit rerank candidates further based on query length/quality.
4. [DONE] Avoid overfetch when reranking is disabled (fetch TopK only).
5. [DONE] Async rerank warmup + cache for recurring queries.
6. [DONE] Add request coalescing for identical searches in flight.
7. [DONE] Reduce logging on hot paths (sampling / debug toggle).
8. [DONE] Use faster hashing for search cache keys (already optimized via maphash).
9. [DONE] Smart cache eviction/admission using TinyLFU.
10. [PENDING] Two‑level caching (in‑memory + distributed) with longer TTLs.
Open‑source alternatives to Redis: Dragonfly or KeyDB (drop‑in), or Garnet (RESP-compatible).
11. [DONE] gRPC streaming for large batch operations (streamed upserts to workers).
12. [DONE] Per‑request search timeout with partial results (skip slow workers).
13. [DONE] Worker batch search RPC + gateway batching to reduce per‑query RPC overhead.
14. [PENDING] Background index warmup for cold start latency.
15. [PENDING] Async result aggregation/streaming for partial results.
16. [PENDING] Memory‑mapped/off‑heap vector storage to reduce GC pressure.
17. [PENDING] Incremental index updates (fine‑grained locking, lazy neighbor updates).
18. [PENDING] Query plan caching (reuse optimal EfSearch/params per fingerprint).
19. [PENDING] Default/auto network compression for large payloads.

## Tier 3 — System/Storage Tuning
1. [DONE] Pebble tuning defaults: memtable, cache size, compaction thresholds.
2. [DONE] Adaptive HNSW snapshot cadence (avoid heavy saves during hot ingest).
3. [DONE] Switch HNSW serialization from gob to a compact binary encoder (done for Raft; extend to snapshots).
4. [DONE] WAL‑only fast recovery for HNSW with periodic snapshots.
5. [DONE] Use bulk loading path for initial indexing.
6. [PENDING] Worker request batching/pipelining to reduce RPC overhead.
7. [PENDING] Placement driver query caching (stale reads for non‑critical lookups).
8. [PENDING] Hot/cold tiered storage with automatic classification.
9. [PENDING] Parallel WAL replay for faster recovery.
10. [PENDING] gRPC connection multiplexing/stream pooling across request types.

## Tier 4 — Architecture / Roadmap
1. [DONE] Per‑collection read consistency policy (eventual vs linearizable).
2. [DONE] Per‑collection rerank policy (enable, top‑N, timeout).
3. [DONE] Optional vector quantization / compression for RAM reduction.
4. [PENDING] Advanced vector compression (PQ/AQ beyond int8).
5. [PENDING] Multi‑tenant isolation (rate limits, resource caps per tenant).
6. [DONE] Async index rebuild / background maintenance scheduling.
7. [DONE] Load‑aware shard placement using worker heartbeat metrics.
8. [DONE] Capacity‑weighted placement (bigger nodes get more shards).
9. [DONE] Failure‑domain aware placement (rack/zone separation).
10. [DONE] Hot‑shard detection with targeted replica moves.
11. [DONE] Background rebalancing with throttling to avoid compaction storms.
12. [PENDING] GPU‑accelerated distance computation for batch queries.
13. [PENDING] Learned index structures for data‑aware search acceleration.
14. [PENDING] Predictive prefetching based on access patterns.
15. [PENDING] Dynamic parameter tuning/auto‑tuning based on live metrics.
16. [PENDING] Vector deduplication to reduce storage for near‑duplicates.

## Notes
- Items marked DONE are already implemented in the codebase (as of current changes).
- Some PENDING items are best validated via benchmark suite (benchmark_research_test.go).
