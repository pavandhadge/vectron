# API Gateway - Reranker Integration (Current)

Last updated: 2026-02-08

## 1. Integration Summary

API Gateway can optionally call the Reranker service after worker search results are collected.

- gateway owns rerank toggle and timeout policy
- reranker returns reordered candidates
- gateway maps reranked output back to public `SearchResponse`

## 2. Control Flags

From `apigateway/cmd/apigateway/config.go`:

- `RERANK_ENABLED` (default `false`)
- `RERANK_TIMEOUT_MS` (default `75`)
- `RERANK_COLLECTIONS` (collection allowlist)
- `RERANK_TOP_N_OVERRIDES`
- `RERANK_TIMEOUT_OVERRIDES`
- `RERANK_WARMUP_ENABLED` and related warmup settings
- `RERANKER_SERVICE_ADDR` (default `localhost:50051`)

## 3. Current Strategy Reality

From `reranker/cmd/reranker/main.go`:

- `rule` strategy is implemented and default
- `llm` and `rl` currently fall back to stub behavior

## 4. Expected Request Path

1. Client calls gateway `Search`.
2. Gateway gathers worker candidates.
3. If reranking is enabled and applicable, gateway calls `RerankService.Rerank`.
4. Gateway returns reranked top results.

## 5. Source of Truth

- `apigateway/cmd/apigateway/main.go`
- `apigateway/cmd/apigateway/config.go`
- `reranker/cmd/reranker/main.go`
- `shared/proto/reranker/reranker.proto`
