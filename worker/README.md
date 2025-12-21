# Worker

The `worker` is the workhorse of the Vectron cluster. It is a stateful service responsible for storing, indexing, and searching vectors. Each worker can manage multiple shards, and each shard is a Raft consensus group, ensuring data replication and fault tolerance.

## Features

*   **Data Storage:** Stores vector data and metadata in **PebbleDB**, a RocksDB-inspired key-value store.
*   **Vector Indexing:** Uses a **Hierarchical Navigable Small World (HNSW)** index for fast and efficient approximate nearest neighbor search.
*   **Shard-level Replication:** Each shard is a Raft consensus group, with its data replicated across multiple worker nodes for high availability and data durability.
*   **Service Discovery:** Registers itself with the `placementdriver` and sends periodic heartbeats to signal its liveness.
*   **Internal API:** Exposes a gRPC API for the `apigateway` to perform data and search operations.

## API

The `worker`'s internal API is defined in `proto/worker/worker.proto`. It provides RPCs for:

*   **Vector Operations:** `StoreVector`, `GetVector`, `DeleteVector`, `Search`.
*   **Key-Value Operations:** `Put`, `Get`, `Delete`.
*   **Admin Operations:** `Status`, `Flush`.

## Building

To build the `worker` binary, run the following command from the root of the project:

```bash
make build-worker
```

The binary will be located at `bin/worker`.
