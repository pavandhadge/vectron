# Reranker Optimization Suggestions

## Summary
Reranking is expensive and should be invoked only when it materially improves quality. The API Gateway already skips reranking when the query is empty, which is a strong default. Below are additional options that can further reduce latency and CPU without sacrificing quality.

## Recommended (High Impact)
1. Feature-flag reranking
   - Gate reranking with an explicit header or config flag (e.g., `X-Rerank: true`) or a per-collection config.
   - This prevents unintended rerank usage and allows opt-in per workload.

2. Candidate cap based on query presence
   - When query text exists, rerank only a small multiple of `TopK` (e.g., `min(TopK*2, 100)`).
   - This is already partially done in the gateway; tighten this further if reranker latency is still high.

3. Timeout + fallback
   - Add a short timeout for rerank calls (e.g., 50–100ms). On timeout, return the original sorted results.
   - This bounds tail latency while keeping quality when reranker is fast.

## Optional
4. Async rerank warmup
   - For frequent queries, asynchronously warm rerank cache on the first request and serve base results immediately.
   - Subsequent requests benefit from cached rerank output.

5. Per-collection policy
   - Allow each collection to specify `rerank_enabled`, `rerank_top_n`, and `rerank_timeout_ms` in metadata.
   - This keeps cost/latency aligned with use-case value.
