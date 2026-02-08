# Vectron Project Overview (Updated)

This document is a code-aligned overview of the current Vectron repository.

For detailed, verified state, see `docs/CURRENT_STATE.md`.

## 1. What Vectron Is

Vectron is a distributed vector database built as Go microservices with gRPC/HTTP APIs, Raft-based control/data consistency, and multi-language SDK support.

Primary goals:

- vector storage and ANN search
- shard-aware routing and replication
- API key/JWT-based access control
- optional reranking and feedback capture
- operational visibility via management endpoints

## 2. Main Components

| Component | Location | Current Role |
|---|---|---|
| API Gateway | `apigateway` | Public API, auth/rate-limit middleware, routes requests to PD/workers, exposes management + feedback endpoints |
| Placement Driver | `placementdriver` | Raft-backed control plane (workers, shards, collections, assignment/rebalance) |
| Worker | `worker` | Stateful data plane (multi-Raft shards, Pebble persistence, HNSW search) |
| Auth Service | `auth/service` | User + API key lifecycle, JWT handling, SDK auth metadata via etcd |
| Reranker | `reranker` | Rerank gRPC service (rule strategy active; llm/rl stubs) |
| Auth Frontend | `auth/frontend` | React management/account console |
| Shared Protos | `shared/proto` | Source of truth for API contracts |
| Client SDKs | `clientlibs/go`, `clientlibs/python`, `clientlibs/js` | Go/Python/JS clients generated/maintained with proto contracts |

## 3. External API Entry Point

The primary user-facing API is `VectronService` in:

- `shared/proto/apigateway/apigateway.proto`

Core operations currently exposed:

- collection lifecycle (`CreateCollection`, `DeleteCollection`, `ListCollections`, `GetCollectionStatus`)
- vector lifecycle (`Upsert`, `Get`, `Delete`)
- search (`Search`, `SearchStream`)
- user plan update (`UpdateUserProfile`)
- feedback (`SubmitFeedback`)

## 4. Runtime Topology

Typical flow:

1. Client calls API Gateway (gRPC or HTTP gateway).
2. Gateway validates identity via Auth Service.
3. Gateway resolves shard/worker placement via Placement Driver.
4. Gateway forwards data/search RPCs to worker replicas.
5. Workers execute writes via shard Raft proposals and reads via shard state.
6. Optional reranking path: Gateway calls Reranker for final result ordering.

## 5. Build and Tooling

Current build orchestration:

- `Makefile`: service builds for Linux/Windows
- `generate-all.sh`: proto generation across services/SDKs
- `go.work`: multi-module workspace

Primary binaries:

- `bin/placementdriver`
- `bin/worker`
- `bin/apigateway`
- `bin/authsvc`
- `bin/reranker`

## 6. Documentation Conventions

- Treat `shared/proto/*` and service entrypoints (`cmd/*/main.go`) as authoritative.
- Use `docs/CURRENT_STATE.md` for current implementation details.
- Older planning/proposal markdown files may describe non-current states.
