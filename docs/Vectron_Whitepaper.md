# Vectron Whitepaper (Exhaustive Codebase Guide, 2026-02-08)

This document is a code-accurate, end-to-end textbook for the Vectron repository. It is intentionally dense and practical: every subsystem, flow, and decision is mapped to concrete files and runtime behaviors. Read in order for full mastery.

## 0. How To Use This Document

- **If you want architecture first**: Sections 1–6.
- **If you want runtime behavior**: Sections 7–14.
- **If you want storage/index internals**: Sections 11–12.
- **If you want SDKs and frontend**: Sections 18–20.
- **If you want tests/benchmarks/ops**: Sections 21–24.

Source of truth: runtime entrypoints + proto contracts + core internal packages.

## 1. System Goals and Invariants

### 1.1 Goals

1. High-throughput vector similarity search with tunable latency/recall.
2. Fault-tolerant data plane via shard-level Raft replication.
3. Control plane separated from data plane.
4. Public API decoupled from internal storage API.
5. Explicit knobs for performance and behavior.

### 1.2 Invariants

1. All writes are serialized per shard via Raft log (linearizable).
2. Shard ownership and placement are managed by PD and converged via reconciliation.
3. Search is approximate by default (HNSW), not exact unless configured.
4. API Gateway is the only public entry point and orchestrates all internal calls.

## 2. Topology and Components

### 2.1 Services

- **API Gateway (`apigateway`)**: Public gRPC + HTTP/JSON; auth, rate limit, routing, cache, rerank, feedback.
- **Placement Driver (`placementdriver`)**: Control plane Raft state for workers, collections, shard placements.
- **Worker (`worker`)**: Data plane shards; Pebble + HNSW; read/write RPCs.
- **Auth (`auth/service`)**: JWT, users, API keys; etcd backend; HTTP gateway.
- **Reranker (`reranker`)**: Post-search ranking; rule strategy active.
- **Auth Frontend (`auth/frontend`)**: React SPA for user + management UI.

### 2.2 Data Plane vs Control Plane

- Control plane = Placement Driver (metadata, shard topology, health).
- Data plane = Workers + Gateway orchestration.

This separation avoids control-plane churn under heavy read/write load.

## 3. Configuration Model (Per-Service Env)

### 3.1 Env File Search Order (All Services)

Each service loads its own env file on startup via `shared/runtimeutil/env.go`:

1. `.env.<service>`
2. `<service>.env`
3. `env/<service>.env`

Existing environment variables take precedence unless `VECTRON_ENV_OVERRIDE=1`.

### 3.2 Dependency Addresses Ownership

Each service stores its dependency addresses in its own env file:

- `env/apigateway.env`: `PLACEMENT_DRIVER`, `AUTH_SERVICE_ADDR`, `RERANKER_SERVICE_ADDR`, `DISTRIBUTED_CACHE_ADDR`, `JWT_SECRET`, gateway ports.
- `env/worker.env`: `PD_ADDRS`.
- `env/auth.env`: `ETCD_ENDPOINTS`, `JWT_SECRET`, ports.
- `env/reranker.env`: `RERANKER_PORT`, strategy, cache backend.
- `env/placementdriver.env`: PD tuning (replication/shards/logdb).

This avoids config leakage across services and ensures separation of concerns.

## 4. Protocol Contracts

### 4.1 Gateway API (`shared/proto/apigateway/apigateway.proto`)

- Collections: `CreateCollection`, `DeleteCollection`, `ListCollections`, `GetCollectionStatus`.
- Data: `Upsert`, `Get`, `Delete`.
- Search: `Search`, `SearchStream`.
- Feedback: `SubmitFeedback`.

HTTP bindings exposed via grpc-gateway.

### 4.2 Worker API (`shared/proto/worker/worker.proto`)

- Write: `StoreVector`, `BatchStoreVector`, `StreamBatchStoreVector`.
- Read/Search: `GetVector`, `DeleteVector`, `Search`, `BatchSearch`.
- Replication: `StreamHNSWSnapshot`, `StreamHNSWUpdates`.

### 4.3 Placement Driver API (`shared/proto/placementdriver/placementdriver.proto`)

- Worker lifecycle: `RegisterWorker`, `Heartbeat`, `ListWorkers`, `DrainWorker`, `RemoveWorker`.
- Routing/control: `GetWorker`, `ListWorkersForCollection`, `Rebalance`.
- Collections: `CreateCollection`, `ListCollections`, `DeleteCollection`, `GetCollectionStatus`.

### 4.4 Auth API (`shared/proto/auth/auth.proto`)

- User: `RegisterUser`, `Login`, `GetUserProfile`, `UpdateUserProfile`, `DeleteUser`, `RefreshToken`.
- API keys: `CreateAPIKey`, `ListAPIKeys`, `DeleteAPIKey`.
- Internal: `ValidateAPIKey`, `CreateSDKJWT`, `GetAuthDetailsForSDK`.

### 4.5 Reranker API (`shared/proto/reranker/reranker.proto`)

- `Rerank`, `GetStrategy`, `InvalidateCache`.

## 5. Data Model

### 5.1 Vector Storage

- Stored in PebbleDB under key prefix `v_`.
- Value encodes vector length + data + metadata (JSON).
- Soft delete = empty vector payload.

### 5.2 Shard Metadata (PD FSM)

- Shard includes ID, key range, replicas, leader, epoch, dimension, distance.
- Collection = name, dimension, distance, shard map.

### 5.3 Routing Key

- Vector ID is hashed into keyspace.
- Gateway chooses shard by key range boundaries.

## 6. Control Plane (Placement Driver)

### 6.1 PD Raft and FSM

- PD itself is a Raft cluster.
- FSM state tracks workers, collections, shards, and membership.
- Commands are JSON-encoded and proposed to Raft.

### 6.2 Worker Registration

- Worker calls `RegisterWorker` with grpc/raft addresses and capabilities.
- PD assigns worker ID and stores role (write vs search-only).

### 6.3 Heartbeat + Assignments

- Worker calls `Heartbeat` every 5s.
- PD updates liveness metrics and responds with `ShardAssignment` list.

### 6.4 Shard Assignment

Assignment includes:

- ShardInfo
- Initial members (bootstrap map)
- Bootstrap flag

### 6.5 Reconciliation Loop

PD runs a background reconciler:

1. Repair under‑replicated shards.
2. Cleanup dead replicas.
3. Cleanup dead workers.
4. Detect leaderless shards.

This ensures eventual convergence to desired replication.

## 7. Data Plane (Worker)

### 7.1 Multi‑Raft Shards

- Each shard is a Raft cluster.
- Dragonboat NodeHost manages all local shards.
- Each shard has its own on‑disk state machine.

### 7.2 ShardManager Lifecycle

- Receives assignments from PD client.
- Starts/stops Raft clusters to match desired state.
- Bootstraps new shards or joins existing ones.

### 7.3 Worker PD Client

- Finds PD leader by probing `ListWorkers`.
- Registers worker and caches worker ID.
- Sends periodic heartbeats and receives assignments.

## 8. State Machine Details

### 8.1 Command Encoding

- `shard.Command` serialized in binary format (fallback JSON + Gob).
- Types: StoreVector, StoreVectorBatch, DeleteVector.

### 8.2 Update Path

- Dragonboat invokes `StateMachine.Update` with committed entries.
- Commands apply to PebbleDB and HNSW.
- `lastApplied` persisted for snapshot state.

### 8.3 Lookup Path

- `SearchQuery` returns IDs + scores.
- `GetVectorQuery` returns vector + metadata.

### 8.4 Snapshot and Recovery

- Snapshot = zipped PebbleDB backup + lastApplied marker.
- Recovery extracts snapshot and reopens Pebble.

## 9. Storage Engine (Pebble)

### 9.1 Initialization

- Opens Pebble with tuning (memtables, cache, bloom).
- Async writes (`pebble.NoSync`) with periodic background sync.
- Optional ingest mode to skip index updates.

### 9.2 Write Path

1. Encode vector + metadata.
2. Write vector + optional WAL in Pebble batch.
3. Commit batch.
4. Update HNSW (sync or async).

### 9.3 Read Path

- `GetVector` reads and decodes Pebble value.
- `ExistsBatch` uses Pebble snapshot for consistent multi-key reads.

### 9.4 WAL + Snapshotting

- HNSW WAL entries stored in Pebble.
- Periodic snapshot of HNSW index.
- WAL replay on startup.

## 10. HNSW Index

### 10.1 Core Algorithm

- Hierarchical graph, greedy search at higher layers.
- Configurable `M`, `EfConstruction`, `EfSearch`.

### 10.2 Vector Representation

- Optional normalization (cosine).
- Optional quantization to int8.
- Optional retention of float vectors for rerank.

### 10.3 Search Variants

- `Search` (default)
- `SearchWithEf` (custom ef)
- `SearchTwoStage` (candidate + rerank)

### 10.4 Maintenance

- Hot index for recent data.
- Pruning of redundant edges.
- Warmup to touch nodes and warm caches.

## 11. Gateway Orchestration

### 11.1 Middleware

- Auth interceptor validates JWT.
- Rate limiting: sharded in-memory counters per user.
- Optional hot-path logging.

### 11.2 Collection Operations

- `CreateCollection` / `DeleteCollection` forwarded to PD.
- Retries on leader change.

### 11.3 Upsert Flow

1. Validate request and each point.
2. Warm routing cache.
3. Resolve shard assignment per point.
4. Batch points per shard.
5. Send batch to worker (streaming for large batches).
6. Retry on stale shard errors with fresh routing.

### 11.4 Search Flow

1. Check rerank warmup cache (if enabled).
2. Check in-memory + distributed cache.
3. Route to workers (fanout or targeted).
4. Worker performs shard search.
5. Gateway aggregates topK with heap.
6. Optional rerank.
7. Cache final response.

### 11.5 SearchStream

- Similar to Search, but streams partial results as workers respond.

### 11.6 Get/Delete Flow

- Resolve shard and forward to worker.
- Retry resolution on stale shard errors.

### 11.7 Feedback Flow

- Feedback stored in SQLite (`apigateway/internal/feedback`).
- Sessions and items with indices for efficient reads.

## 12. Search‑Only Mode

- Worker can run with `VECTRON_SEARCH_ONLY=1`.
- No writes; serves queries from replicated HNSW state.
- Gateway can prefer search‑only workers for non‑linearizable reads.

## 13. Reranker

### 13.1 Strategy Model

- `Strategy` interface, rule strategy implemented.
- LLM/RL strategies stubbed.

### 13.2 Cache

- Cache key includes query + candidate IDs + options.
- Memory or Redis backend.

### 13.3 Gateway Integration

- Enabled only when `RERANK_ENABLED=true` and query is non‑empty.
- Timeout configurable with overrides.
- Fail-open (returns original order if rerank fails).

## 14. Auth Service

### 14.1 Storage

- etcd backend, user and API key records.

### 14.2 JWT

- Login JWT includes user ID and plan.
- SDK JWT includes API key ID; gateway resolves via Auth RPC.

### 14.3 API Keys

- Stored hashed.
- Full key returned only once on creation.

## 15. Caching Model

- Gateway: TinyLFU + TTL and optional Redis/Valkey.
- Worker: in-memory search response cache with quantized key option.
- Reranker: per-query candidate cache.

## 16. Consistency and Durability

- Writes: Raft SyncPropose in shard.
- Reads: optional linearizable via SyncRead.
- Search: approximate, tunable for recall/latency.
- Durability: Pebble WAL + periodic sync + HNSW snapshots.

## 17. Observability and Profiling

- Hot-path log sampling via env.
- pprof hooks for CPU/mutex/block.
- Gateway management endpoints (`/v1/admin/*`, `/v1/system/health`).

## 18. SDKs (Client Libraries)

### 18.1 Go Client (`clientlibs/go/client.go`)

- gRPC client with TLS, keepalive, retries, hedged reads.
- `ClientOptions` controls timeouts, compression, retry policy.
- Implements `Upsert`, `Search`, `CreateCollection`, etc.

### 18.2 Python Client (`clientlibs/python/vectron_client/client.py`)

- gRPC client with retries, hedged reads, vector dimension validation.
- Errors mapped to typed exceptions.

### 18.3 JS/TS Client (`clientlibs/js/src/client.ts`)

- gRPC-js client with typed errors and options.
- Similar retry and hedging semantics.

### 18.4 Code Generation (`generate-all.sh`)

- Centralized protobuf generation for Go, JS, Python, and OpenAPI.

## 19. Frontend (Auth Console)

### 19.1 App Structure

- `auth/frontend/src/App.tsx`: Router + layout.
- `AuthProvider`: Axios clients + auth state management.

### 19.2 Key Pages

- User dashboard, API keys, profile, billing, collections.
- Management console: gateway stats, workers, collections, system health.

### 19.3 API Clients

- `AuthContext`: JWT injection into HTTP requests.
- `managementApi`: admin endpoints on gateway.

## 20. Tooling and Scripts

- `run-all.sh`: starts full local stack, services self-load env files.
- `generate-all.sh`: proto generation for all languages.
- `profile-benchmark.sh`: perf harness.

## 21. Tests

### 21.1 End‑to‑End

- `tests/e2e/ultimate_e2e_test.go`: full system lifecycle, auth, rerank, recovery.
- `tests/e2e/e2e_*`: additional coverage.

### 21.2 Benchmarks

- `tests/benchmark/benchmark_research_test.go`: full metrics suite (latency, recall, NDCG, throughput).

## 22. Performance Tuning

- Tunables documented in `ENV_SAMPLE.env`.
- Core knobs: ef parameters, async indexing, cache sizes, fanout, rerank timeouts.

## 23. Failure Handling

- PD leader changes: gateway and workers retry.
- Stale shard errors: gateway refreshes routing.
- Under-replication: PD reconciler repairs.

## 24. Mastery Checklist

1. Trace `Upsert` from gateway → worker → raft → storage.
2. Trace `Search` from gateway → workers → HNSW → aggregation → rerank.
3. Understand PD’s shard assignment and reconciliation logic.
4. Tune HNSW for latency/recall tradeoff.
5. Know caching behavior at gateway/worker/reranker.
6. Understand auth model and JWT handling.
7. Know how SDKs map to proto APIs.

