# Reranker Optimization (Current)

Last updated: 2026-02-08

## 1. Current Implementation Status

- Reranker service is active and integrated with API Gateway.
- `rule` strategy is implemented and default.
- `llm` and `rl` strategy selections currently fall back to stub implementations.

## 2. Runtime Controls

Reranker service controls:

- `RERANKER_PORT`
- `RERANKER_STRATEGY`
- `CACHE_BACKEND` (`memory` or `redis`)
- `RULE_CONFIG_PATH` and rule env parameters

Gateway-side rerank controls:

- `RERANK_ENABLED`
- `RERANK_TIMEOUT_MS`
- `RERANK_COLLECTIONS`
- override maps for `TopN` and timeout
- rerank warmup flags

## 3. Practical Optimization Priorities

1. Keep `rule` strategy for production paths until non-stub alternatives are implemented.
2. Tune timeout and collection allowlists in gateway first.
3. Enable cache backend that matches deployment profile (memory for simple, redis for multi-instance).
4. Measure p95/p99 rerank latency before increasing rerank scope.

## 4. Measurable Checks

- gateway request latency with rerank on/off
- reranker service latency and error rate
- cache hit/miss ratio
- impact on search relevance metrics from feedback datasets

## 5. Source of Truth

- `reranker/cmd/reranker/main.go`
- `reranker/internal/strategies/rule/*`
- `apigateway/cmd/apigateway/config.go`
- `shared/proto/reranker/reranker.proto`
