# Vectron Current Code State

This document reflects the repository state as of February 8, 2026, based on current code under:

- `apigateway/cmd/apigateway/main.go`
- `apigateway/cmd/apigateway/config.go`
- `worker/cmd/worker/main.go`
- `placementdriver/cmd/placementdriver/main.go`
- `auth/service/cmd/auth/main.go`
- `reranker/cmd/reranker/main.go`
- `shared/proto/*`
- `Makefile`
- `ENV_SAMPLE.env`
- `env/*.env`

## 1. Active Services

- API Gateway (`apigateway`): public API (gRPC + HTTP via grpc-gateway), auth/rate-limit middleware, routing to PD/workers, feedback ingestion, management endpoints, optional reranking and caching.
- Placement Driver (`placementdriver`): Raft-backed control plane, worker registration/heartbeats, shard assignment, collection metadata, rebalancing/drain APIs.
- Worker (`worker`): data plane service using multi-Raft shards + Pebble + HNSW; supports standard and search-only modes.
- Auth Service (`auth/service`): user/account + API key service on etcd, JWT-based protected endpoints, HTTP gateway.
- Reranker (`reranker`): pluggable reranking service (rule active; llm/rl currently stubs), memory/redis cache backends.
- Auth Frontend (`auth/frontend`): React SPA with account pages plus management console pages.

## 2. Public API Surface (Gateway)

From `shared/proto/apigateway/apigateway.proto`:

- `CreateCollection`
- `DeleteCollection`
- `Upsert`
- `Search`
- `SearchStream` (streaming search response)
- `Get`
- `Delete`
- `ListCollections`
- `GetCollectionStatus`
- `UpdateUserProfile`
- `SubmitFeedback`

HTTP bindings include:

- `POST /v1/collections`
- `DELETE /v1/collections/{name}`
- `POST /v1/collections/{collection}/points`
- `POST /v1/collections/{collection}/points/search`
- `GET /v1/collections/{collection}/points/{id}`
- `DELETE /v1/collections/{collection}/points/{id}`
- `GET /v1/collections`
- `GET /v1/collections/{name}/status`
- `PUT /v1/user/profile`
- `POST /v1/feedback`

## 3. Internal APIs

Placement Driver (`shared/proto/placementdriver/placementdriver.proto`):

- worker lifecycle: `RegisterWorker`, `Heartbeat`, `ListWorkers`, `DrainWorker`, `RemoveWorker`
- routing/control: `GetWorker`, `ListWorkersForCollection`, `Rebalance`
- collections: `CreateCollection`, `ListCollections`, `DeleteCollection`, `GetCollectionStatus`
- cluster info: `GetLeader`

Worker (`shared/proto/worker/worker.proto`):

- data ops: `StoreVector`, `BatchStoreVector`, `StreamBatchStoreVector`, `GetVector`, `DeleteVector`
- search ops: `Search`, `BatchSearch`
- replication/index sync: `StreamHNSWSnapshot`, `StreamHNSWUpdates`
- utility KV/status: `Put`, `Get`, `Delete`, `Status`, `Flush`

Auth (`shared/proto/auth/auth.proto`):

- user: `RegisterUser`, `Login`, `GetUserProfile`, `UpdateUserProfile`, `DeleteUser`, `RefreshToken`
- key lifecycle: `CreateAPIKey`, `ListAPIKeys`, `DeleteAPIKey`
- internal/sdk: `ValidateAPIKey`, `CreateSDKJWT`, `GetAuthDetailsForSDK`

Reranker (`shared/proto/reranker/reranker.proto`):

- `Rerank`, `GetStrategy`, `InvalidateCache`

## 4. Runtime Endpoints and Defaults

API Gateway (`apigateway/cmd/apigateway/config.go`):

- `GRPC_ADDR` default `:8081`
- `HTTP_ADDR` default `:8080`
- connects to `PLACEMENT_DRIVER`, `AUTH_SERVICE_ADDR`, `RERANKER_SERVICE_ADDR`
- feedback DB path via `FEEDBACK_DB_PATH`
- rich cache/rerank toggles via env vars (see `ENV_SAMPLE.env`)
- env file search order: `.env.apigateway`, `apigateway.env`, `env/apigateway.env`

Placement Driver (`placementdriver/cmd/placementdriver/main.go`):

- default gRPC `localhost:6001`
- default raft `localhost:7001`
- supports multi-node bootstrap with `-initial-members`

Worker (`worker/cmd/worker/main.go`):

- default gRPC `localhost:9090`
- default raft `localhost:9191`
- PD addrs via `-pd-addrs` (default from `PD_ADDRS` if set)
- supports `VECTRON_SEARCH_ONLY=1`
- env file search order: `.env.worker`, `worker.env`, `env/worker.env`

Auth Service (`auth/service/cmd/auth/main.go`):

- gRPC default `:8081`, HTTP default `:8082`
- etcd default `localhost:2379`
- `JWT_SECRET` is required and must be >= 32 characters
- env file search order: `.env.auth`, `auth.env`, `env/auth.env`

Reranker (`reranker/cmd/reranker/main.go`):

- default port `50051`
- default strategy `rule`
- `llm` and `rl` options currently fall back to stub implementations
- env file search order: `.env.reranker`, `reranker.env`, `env/reranker.env`

Placement Driver (`placementdriver/cmd/placementdriver/main.go`):

- default gRPC `localhost:6001`
- default raft `localhost:7001`
- supports multi-node bootstrap with `-initial-members`
- env file search order: `.env.placementdriver`, `placementdriver.env`, `env/placementdriver.env`

## 5. Management Console Reality Check

Implemented backend management endpoints in API Gateway include:

- `GET /v1/system/health`
- `GET /v1/admin/stats`
- `GET /v1/admin/workers`
- `GET /v1/admin/collections`
- `GET /v1/alerts`
- `POST /v1/alerts/{id}/resolve` (handled by suffix check)

Frontend routes exist for:

- `/dashboard/management`
- `/dashboard/management/gateway`
- `/dashboard/management/workers`
- `/dashboard/management/collections`
- `/dashboard/management/health`

The in-app `Documentation` page (`auth/frontend/src/pages/Documentation.tsx`) is currently mock/demo content and should not be used as technical truth.

## 6. Build/Run State

From `Makefile`:

- build targets exist for all five services (`placementdriver`, `worker`, `apigateway`, `authsvc`, `reranker`)
- Linux and Windows builds are supported
- worker Linux build uses `-tags avx512` by default through `AVX512_TAGS`

From root scripts:

- `generate-all.sh` exists for proto generation
- `run-all.sh` exists for local orchestration

## 7. Current Known Gaps vs. Typical Old Docs

- Reranker service exists and is integrated, but only rule strategy is implemented; llm/rl remain stubs.
- Management console backend endpoints exist, but some frontend pages still rely on partial/fallback behavior.
- Historical docs prior to the 2026-02-08 refresh may still appear in commit history; use current `docs/` files plus proto/entrypoint code for authoritative behavior.

## 8. Documentation Policy for This Repo

- Use `shared/proto/*` as API source of truth.
- Use each service `cmd/*/main.go` and config files for runtime defaults.
- Treat older design/proposal markdown files as non-authoritative unless explicitly updated.
