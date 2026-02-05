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

## Tier 1 — Major Wins
1. [DONE] Remove per‑candidate Pebble existence checks in search.
2. [DONE] Replace JSON Raft commands with compact binary encoding (backward compatible).
3. [DONE] Reduce HNSW per‑query allocations using pools.
4. [DONE] Cache vector norms for cosine distance to reduce CPU per distance calc.
5. [DONE] Reduce gRPC overhead with keepalive/window/buffer tuning across services.
6. [DONE] Fast path: avoid map creation for empty payloads end‑to‑end.
7. [DONE] gRPC response compression for large responses (evaluate CPU vs bandwidth).

## Tier 2 — Medium Wins
1. [DONE] Worker list cache in API Gateway (short TTL).
2. [DONE] Worker resolve cache in API Gateway (short TTL).
3. [DONE] Limit rerank candidates further based on query length/quality.
4. [DONE] Avoid overfetch when reranking is disabled (fetch TopK only).
4. [DONE] Async rerank warmup + cache for recurring queries.
5. [DONE] Add request coalescing for identical searches in flight.
6. [DONE] Reduce logging on hot paths (sampling / debug toggle).
7. [DONE] Use faster hashing for search cache keys (already optimized via maphash).

## Tier 3 — System/Storage Tuning
1. [DONE] Pebble tuning defaults: memtable, cache size, compaction thresholds.
2. [DONE] Adaptive HNSW snapshot cadence (avoid heavy saves during hot ingest).
3. [DONE] Switch HNSW serialization from gob to a compact binary encoder (done for Raft; extend to snapshots).
4. [DONE] WAL‑only fast recovery for HNSW with periodic snapshots.
5. [DONE] Use bulk loading path for initial indexing.

## Tier 4 — Architecture / Roadmap
1. [DONE] Per‑collection read consistency policy (eventual vs linearizable).
2. [DONE] Per‑collection rerank policy (enable, top‑N, timeout).
3. [DONE] Optional vector quantization / compression for RAM reduction.
4. [PENDING] Multi‑tenant isolation (rate limits, resource caps per tenant).
5. [DONE] Async index rebuild / background maintenance scheduling.
6. [PENDING] Load‑aware shard placement using worker heartbeat metrics.
7. [PENDING] Capacity‑weighted placement (bigger nodes get more shards).
8. [PENDING] Failure‑domain aware placement (rack/zone separation).
9. [PENDING] Hot‑shard detection with targeted replica moves.
10. [PENDING] Background rebalancing with throttling to avoid compaction storms.

## Notes
- Items marked DONE are already implemented in the codebase (as of current changes).
- Some PENDING items are best validated via benchmark suite (benchmark_research_test.go).
