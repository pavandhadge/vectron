# Vectron Architecture Deep Dive

Last updated: 2026-02-08

This document is written as a website-ready technical deep dive. It explains how Vectron works today, why it is designed this way, and where the important architectural tradeoffs live.

Authoritative implementation sources:

- `shared/proto/*`
- `apigateway/cmd/apigateway/main.go`
- `apigateway/cmd/apigateway/config.go`
- `placementdriver/cmd/placementdriver/main.go`
- `placementdriver/internal/server/*`
- `worker/cmd/worker/main.go`
- `worker/internal/shard/*`
- `worker/internal/storage/*`
- `auth/service/cmd/auth/main.go`
- `reranker/cmd/reranker/main.go`

## 1. Architectural Intent

Vectron is a distributed vector database with a strong separation between:

- control plane decisions (metadata, placement, cluster topology)
- data plane operations (vector writes, ANN search)
- edge/API concerns (auth, protocol translation, fanout, caching)

This split is deliberate. It reduces coupling, lets each plane evolve independently, and makes operational failures easier to isolate.

## 2. Service Boundaries and Responsibilities

### 2.1 API Gateway (`apigateway`)

Primary responsibilities:

- public API surface (`VectronService`) via gRPC and HTTP/JSON
- auth and rate-limiting middleware
- placement lookup + worker routing
- search fanout and result aggregation
- optional reranking integration
- feedback ingestion (SQLite)
- management endpoints for operations UI

Design role: stateless orchestrator and policy enforcement layer.

### 2.2 Placement Driver (`placementdriver`)

Primary responsibilities:

- cluster metadata authority (workers, collections, shard maps)
- worker lifecycle: register, heartbeat, state transitions (ready/draining)
- routing resolution (`GetWorker`, worker lists)
- replication health reconciliation
- background rebalancing orchestration

Design role: Raft-backed control plane coordinator.

### 2.3 Worker (`worker`)

Primary responsibilities:

- shard replica hosting (multi-Raft)
- write path through Raft proposals
- search/get/delete read paths per shard
- local persistence with Pebble
- in-memory ANN index (HNSW) and snapshot/WAL support

Design role: authoritative shard data plane.

### 2.4 Auth Service (`auth/service`)

Primary responsibilities:

- user identity lifecycle
- JWT session issuance and validation
- API key lifecycle and validation
- SDK JWT wrapping and auth detail lookup
- etcd-backed credential state

Design role: centralized trust and identity service.

### 2.5 Reranker (`reranker`)

Primary responsibilities:

- post-retrieval reranking RPC API
- strategy abstraction and cache backends
- rule strategy as active production path

Design role: optional relevance enhancement stage.

## 3. Contract Model and Why It Matters

Vectron treats `.proto` files as system contracts. The key decision is explicit anti-corruption boundaries:

- public API contract in `apigateway.proto`
- internal worker contract in `worker.proto`
- gateway translator converts between them

Why this decision:

- protects SDK clients from internal schema churn
- lets worker internals evolve without breaking external callers
- keeps protocol-level compatibility decisions centralized

Tradeoff:

- translation layer must stay complete; missing mappings become data-loss/feature gaps
- today, payload/metadata translation has TODO gaps in translator paths

## 4. End-to-End Flows

### 4.1 Worker Join and Placement Enrollment

1. Worker process starts (`worker/cmd/worker/main.go`).
2. Worker creates PD client and discovers current PD leader.
3. Worker calls `RegisterWorker` with addresses + capacity/failure-domain metadata.
4. PD persists worker registration via Raft proposal to FSM.
5. Worker begins heartbeat loop (default 5s interval).
6. Heartbeat response carries serialized shard assignments.
7. Worker shard manager reconciles local running shards against desired assignments.

Why this design:

- pulls desired-state from control plane on every heartbeat
- avoids a separate assignment stream protocol
- naturally self-heals after transient network failures

### 4.2 Collection Creation

1. Client calls Gateway `CreateCollection`.
2. Gateway forwards to PD `CreateCollection`.
3. PD Raft-commits metadata update.
4. FSM creates shard map using configured initial shard count.
5. Shards are assigned to workers based on current worker set and replication policy.
6. Workers learn/act on assignments through heartbeats.

Consistency property:

- collection metadata is control-plane linearizable (Raft proposal path).

### 4.3 Upsert (Write Path)

1. Client calls Gateway `Upsert` with points.
2. Gateway resolves each vector ID to shard/worker via PD lookup and caches.
3. Gateway translates request shape to worker contract.
4. Gateway forwards to target worker RPC (`StoreVector`/batch variants).
5. Worker proposes command to shard Raft group.
6. On commit, shard state machine applies write to storage/index.

Consistency property:

- write acknowledgment occurs after shard-Raft commit semantics.

Important current behavior:

- translator currently leaves payload/metadata mapping as placeholders in some paths.
- this is a known contract-to-storage gap and should be treated as high-priority hardening.

### 4.4 Search (Read Path)

1. Client calls Gateway `Search` (or `SearchStream`).
2. Gateway determines search routing strategy (fanout, caching, consistency policy).
3. Gateway dispatches worker search RPCs.
4. Worker executes shard search (linearizable or non-linearizable based on request/policy).
5. Gateway merges candidate results.
6. Optional: Gateway calls Reranker and returns reranked subset.

Search consistency model:

- default is non-linearizable for latency (`SEARCH_LINEARIZABLE=false`)
- can be overridden globally/per collection/per request patterns

Why this design:

- gives operators explicit latency vs. consistency control
- supports high-throughput retrieval workloads with optional stronger semantics

### 4.5 Search Streaming

`SearchStream` exists to progressively return results, reducing user-perceived latency under fanout.

Why this matters:

- partial useful responses can arrive before slowest shard/workers complete
- useful for interactive UIs and latency-sensitive agent loops

### 4.6 Feedback Capture

1. Client submits `SubmitFeedback`.
2. Gateway validates request and persists feedback via internal feedback service.
3. Data is stored in SQLite at configured path.

Current scope:

- ingestion and storage are implemented
- full online-learning feedback loop is not yet implemented in this repo

### 4.7 Worker Drain and Rebalance

1. Operator marks worker draining (`DrainWorker`).
2. PD control-plane state changes via Raft.
3. Reconciliation/rebalancing logic identifies safe moves.
4. Replica add/remove and membership progression are orchestrated with safety checks.

Notable sophistication:

- rebalancer tracks in-flight move phases and retries with backoff
- checks applied index and leader-transfer acknowledgments for safer move completion
- compaction-aware throttling/backoff avoids worsening storage pressure

## 5. Consistency Architecture

### 5.1 Control Plane Consistency

PD uses Raft for metadata mutations:

- worker membership/state
- collection definitions
- shard replica mappings
- leadership metadata updates

Guarantee:

- committed control state is consistent across PD quorum.

### 5.2 Data Plane Consistency

Each shard is an independent Raft group hosted on workers.

Guarantee:

- writes replicate through shard quorum before apply.

### 5.3 Read Semantics

Reads are policy-driven:

- linearizable reads available where required
- eventual/non-linearizable reads available for lower latency

This mixed model is intentional, not accidental.

## 6. Performance Architecture

### 6.1 Gateway Caching Strategy

Gateway uses layered caches and de-duplication primitives, including:

- search result cache
- worker list cache
- routing cache
- worker resolve cache
- optional distributed cache (Redis/Valkey)
- in-flight request coalescing

Why this design:

- avoid repeated PD and worker lookups for hot keys
- reduce duplicate concurrent work
- keep high-percentile latency stable under burst traffic

### 6.2 Worker Data/Index Strategy

Worker combines:

- Pebble for durable KV
- HNSW for ANN retrieval
- snapshot and WAL/streaming mechanisms for index/state continuity

Why this design:

- Pebble gives efficient local persistence and compaction behavior
- HNSW gives practical low-latency ANN quality/performance
- explicit snapshot/stream channels help replica convergence paths

### 6.3 Runtime Tuning Surface

`ENV_SAMPLE.env` lists tunables; active configs are loaded from per-service env files (`.env.<service>`, `<service>.env`, or `env/<service>.env`) for:

- gRPC sizing
- cache TTL/capacity
- fanout controls
- rerank controls
- storage/index behavior
- profiling hooks (`PPROF_*`)

This is a key operational philosophy: tuning is explicit and externalized.

## 7. Security Architecture

### 7.1 Token Model

Gateway auth middleware distinguishes two token classes:

- login JWTs (user session semantics)
- SDK JWTs containing API key identity references

For SDK JWTs, gateway calls Auth Service to resolve authoritative user/plan context.

### 7.2 API Key Handling

Auth Service stores hashed API keys and returns full keys only at creation time.

Rationale:

- reduce blast radius of credential-store compromise
- follow standard secret-handling patterns

### 7.3 Current Risk Notes

- management HTTP endpoints are implemented in gateway and should be protected by deployment controls (network policy, reverse proxy auth, or gateway auth layer extension), depending on environment.

## 8. Management and Observability

API Gateway management module provides:

- system health aggregation
- endpoint/request/error metrics
- worker and collection introspection
- alert listing/resolution operations

This supports the Auth frontend management console pages.

Current tradeoff:

- some metrics are placeholders/derived approximations until deeper telemetry pipelines are added.

## 9. Design Decisions (Explicit)

1. Microservice separation instead of monolith.
2. Proto-first contracts as system-of-record interfaces.
3. API Gateway as edge orchestrator, not data owner.
4. Dedicated control plane service (PD) instead of embedded placement logic.
5. Multi-Raft sharding in workers for independent shard fault domains.
6. Policy-selectable read consistency rather than one fixed read mode.
7. Layered caching at gateway to absorb hot paths.
8. Optional reranker integration to decouple retrieval and ranking concerns.
9. Feedback collection in gateway for future relevance loops.
10. Environment-driven tuning to enable workload-specific operation.
11. Reconciliation + rebalancing loops in PD to reduce manual ops burden.
12. Role-aware workers (write/search_only) to support topology specialization.
13. Heartbeat-based assignment delivery for eventual convergence without extra stream dependency.
14. Anti-corruption translator between public/internal vector contracts.
15. Built-in graceful shutdown/timeouts in control-plane servers.

## 10. Known Gaps and Open Edges

- payload/metadata translation in gateway translator is incomplete in current code paths.
- reranker `llm`/`rl` strategies are stub fallbacks today.
- some management metrics are currently synthetic/partial.
- comprehensive cross-service auth policy on management routes may need explicit hardening per deployment.

These are implementation realities, not theoretical limitations.

## 11. How to Read This Architecture as a Contributor

When changing architecture, validate in this order:

1. contract impact (`shared/proto/*`)
2. control-plane impact (PD FSM/proposals)
3. data-plane impact (worker shard/state machine)
4. edge impact (gateway middleware/routing/cache)
5. operational impact (env vars, defaults, observability)

If a change breaks one layer’s assumptions, document it as a decision update before merging.

## 12. One-Page Summary

Vectron is designed around an explicit, pragmatic split:

- PD decides placement and cluster truth.
- Workers own replicated vector state.
- Gateway orchestrates client-facing execution with policy and performance controls.
- Auth centralizes trust.
- Reranker remains optional and composable.

The system favors operational clarity, explicit tradeoffs, and evolvable contracts over opaque "magic" behavior.
