# Worker Service (Current)

Last updated: 2026-02-08

## 1. What It Does

Worker is the Vectron data plane node.

- hosts shard replicas
- persists vectors and metadata
- serves search and vector CRUD
- runs shard-level Raft state machines
- supports optional search-only mode

## 2. Runtime Modes

Standard mode (default):

- Dragonboat NodeHost initialized
- shard manager syncs assignments from PD
- writes and replicated state managed by shard Raft groups

Search-only mode (`VECTRON_SEARCH_ONLY=1`):

- no local raft ownership
- serves search from streamed/index-synced state

## 3. Runtime Defaults and Env Loading

From `worker/cmd/worker/main.go`:

- `-grpc-addr` default `localhost:9090`
- `-raft-addr` default `localhost:9191`
- `-pd-addrs` default `PD_ADDRS` if set, otherwise `localhost:6001`
- `-node-id` default `1`
- `-data-dir` default `./worker-data`

Common gRPC sizing env vars:

- `GRPC_MAX_RECV_MB` (default 256)
- `GRPC_MAX_SEND_MB` (default 256)
- `GRPC_MAX_STREAMS` (default 1024)

Env files are loaded on startup in this order (first match wins):
1. `.env.worker`
2. `worker.env`
3. `env/worker.env`

Many storage/index tuning vars are documented in `ENV_SAMPLE.env`.

## 4. API Surface

From `shared/proto/worker/worker.proto`:

- write APIs: `StoreVector`, `BatchStoreVector`, `StreamBatchStoreVector`
- read/search APIs: `GetVector`, `DeleteVector`, `Search`, `BatchSearch`
- replication/index streaming: `StreamHNSWSnapshot`, `StreamHNSWUpdates`
- utility methods: `Put`, `Get`, `Delete`, `Status`, `Flush`

## 5. Storage and Indexing

- persistent KV: Pebble
- vector index: HNSW (`worker/internal/idxhnsw`)
- raft/state-machine integration under `worker/internal/shard`
- optional WAL stream support via `VECTRON_WAL_STREAM_ENABLED`

## 6. Source of Truth

- `worker/cmd/worker/main.go`
- `worker/internal/grpc.go`
- `worker/internal/shard/*`
- `worker/internal/storage/*`
- `shared/proto/worker/worker.proto`
