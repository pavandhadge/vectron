# Vectron Architecture (Updated)

This architecture summary reflects the code currently in this repository.  
Detailed service/runtime facts are in `docs/CURRENT_STATE.md`.

## 1. High-Level Architecture

```text
Client SDKs / HTTP Clients
          |
          v
     API Gateway
   (gRPC + HTTP API)
      /    |     \
     /     |      \
    v      v       v
 Auth   Placement  Reranker (optional)
Service  Driver
           |
           v
        Workers (N)
      (Multi-Raft shards,
       Pebble + HNSW)
```

## 2. Service Responsibilities

- API Gateway:
  - exposes public Vectron API (`shared/proto/apigateway/apigateway.proto`)
  - applies auth/rate-limit middleware
  - resolves worker targets through Placement Driver
  - forwards data/search requests to workers
  - exposes management and alert endpoints
  - persists user feedback to SQLite
- Placement Driver:
  - Raft-backed control plane metadata
  - worker registration and heartbeat processing
  - shard assignment and rebalancing actions
  - collection metadata and routing support
- Worker:
  - hosts shard replicas under Dragonboat
  - executes writes through Raft proposals
  - serves vector search/read paths per shard
  - stores vectors/index state using Pebble + HNSW
- Auth Service:
  - user lifecycle and JWT issuance
  - API key lifecycle and validation
  - etcd-backed storage
- Reranker:
  - reranking RPC surface
  - rule strategy implemented
  - llm/rl strategy options currently stubbed

## 3. Request Flows

### Create Collection

1. Client calls `CreateCollection` on API Gateway.
2. Gateway forwards to Placement Driver.
3. Placement Driver commits metadata via Raft and returns result.

### Upsert

1. Client calls `Upsert`.
2. Gateway resolves shard + worker via Placement Driver.
3. Gateway forwards translated request to worker.
4. Worker proposes write to shard Raft group and applies to local state machine.

### Search

1. Client calls `Search` (or `SearchStream`).
2. Gateway resolves candidate workers/shards via Placement Driver and local caches.
3. Gateway fans out/forwards search RPCs to workers and merges results.
4. If reranking is enabled for the request/collection, Gateway calls Reranker.
5. Gateway returns final response.

## 4. Management and Ops Endpoints

API Gateway includes HTTP management handlers:

- `/v1/system/health`
- `/v1/admin/stats`
- `/v1/admin/workers`
- `/v1/admin/collections`
- `/v1/alerts`
- `/v1/alerts/{id}/resolve`

These feed the React management pages under `auth/frontend/src/pages/*Management*.tsx`.

## 5. Data/Control Planes

- Control plane:
  - Placement Driver Raft cluster
  - worker assignments, collection metadata, rebalancing
- Data plane:
  - workers hosting replicated shards
  - vector CRUD + ANN search
  - gateway as request orchestration/routing layer

## 6. Source of Truth

When this file conflicts with implementation, prioritize:

1. `shared/proto/*`
2. `*/cmd/*/main.go`
3. service internal packages used by those entrypoints
