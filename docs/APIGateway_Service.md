# API Gateway Service (Current)

Last updated: 2026-02-08

## 1. What It Does

The API Gateway is the public entry point for Vectron.

- serves gRPC and HTTP/JSON APIs
- authenticates requests via Auth Service
- applies rate limiting and request logging middleware
- resolves worker targets through Placement Driver
- forwards vector operations to workers
- optionally reranks search results through Reranker
- stores feedback in SQLite
- exposes management endpoints for the auth frontend console

## 2. Runtime Defaults

From `apigateway/cmd/apigateway/config.go`:

- `GRPC_ADDR=:8081`
- `HTTP_ADDR=:8080`
- `PLACEMENT_DRIVER=placement:6300`
- `AUTH_SERVICE_ADDR=auth:50051`
- `RERANKER_SERVICE_ADDR=localhost:50051`
- `FEEDBACK_DB_PATH=./data/feedback.db`
- `RATE_LIMIT_RPS=100`

Additional performance/caching/rerank env vars are documented in `ENV_SAMPLE.env`.

## 3. Key Internal Behaviors

- PD leader handling: gateway retries PD nodes and updates leader on transient failures.
- Worker routing cache layers: in-memory plus optional distributed cache.
- Search behavior:
  - fanout support across workers/shards
  - optional per-request timeout (`SearchRequest.timeout_ms`)
  - optional reranking when enabled by config
- Middleware chain:
  - auth interceptor
  - rate limit interceptor
  - logging interceptor (disabled in raw speed mode)

## 4. Exposed Endpoints

Public HTTP endpoints are generated from `apigateway.proto`.

Management endpoints (served by gateway HTTP server):

- `GET /v1/system/health`
- `GET /v1/admin/stats`
- `GET /v1/admin/workers`
- `GET /v1/admin/collections`
- `GET /v1/alerts`
- `POST /v1/alerts/{id}/resolve`

## 5. Known Current Constraints

- Distributed cache is optional and only active when configured.
- Reranking is disabled by default (`RERANK_ENABLED=false`).
- Rate limiting is process-local (not globally shared across gateway instances).

## 6. Source of Truth

- `apigateway/cmd/apigateway/main.go`
- `apigateway/cmd/apigateway/config.go`
- `shared/proto/apigateway/apigateway.proto`
