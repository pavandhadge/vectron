# Vectron Worker

The `worker` is the stateful workhorse of the Vectron cluster. It is a single service responsible for storing vector data, building and managing vector indexes, and executing search queries. The worker is designed to be a horizontally scalable, fault-tolerant data node.

## Architecture and Design

The worker's architecture is a sophisticated composition of a key-value store, a vector index, and a "multi-Raft" consensus system. This enables it to manage multiple, independent, replicated data shards simultaneously.

### 1. Multi-Raft Sharding

A single worker node can host replicas for many different shards. Each shard is its own independent Raft consensus group, managed by a single, shared [Dragonboat](https://github.com/lni/dragonboat) `NodeHost` process.

- **Shard Manager:** A `ShardManager` component runs on each worker. It communicates with the `placementdriver` via a heartbeat mechanism. The placement driver's response tells the manager which shards (and their replicas) the worker is expected to host. The manager's job is to reconcile this desired state, starting and stopping shard Raft groups as commanded by the PD.
- **Data Replication:** All write operations for a shard (e.g., `StoreVector`) are submitted as `Command`s to the shard's Raft log using a linearizable `SyncPropose`. The command is only applied to the state machine after being replicated to a quorum of the shard's replicas (which are hosted on other worker nodes). This guarantees that the data for each shard is durable and consistent across failures.

### 2. Per-Shard State Machine

Each shard (Raft group) has its own instance of a `StateMachine`. This is the core of the data layer.

- **Composition:** The state machine is built directly on top of a storage engine. It embeds the database client, meaning the state machine _is_ the interface to the physical storage for that shard.
- **Write Path:** The `Update` method of the state machine is called by Dragonboat to apply a committed Raft log entry. It decodes the `Command` (e.g., `StoreVector`) and calls the corresponding write method on the underlying storage engine.
- **Read Path:** The `Lookup` method is used for linearizable reads. The gRPC server uses Dragonboat's `SyncRead` to invoke this method, which in turn calls the appropriate read method on the storage engine (e.g., performing a vector search).

### 3. Storage and Indexing Engine

Each state machine instance manages its own dedicated storage on disk.

- **Key-Value Store:** The worker uses **PebbleDB** (a RocksDB-inspired key-value store written in Go) as its underlying storage engine. Raw vector data and metadata are stored here.
- **Vector Index:** For efficient similarity search, the worker uses a **Hierarchical Navigable Small World (HNSW)** index. The `idxhnsw` package provides an in-memory implementation of this algorithm.
- **Index Persistence:** The in-memory HNSW index must be persisted durably. The storage engine uses a two-pronged strategy:
  1.  **Snapshotting:** Periodically, the entire HNSW graph is serialized and saved as a single value within PebbleDB.
  2.  **Write-Ahead Log (WAL):** Every modification to the HNSW index (add or delete) is also written as a small, fast entry into PebbleDB.
      On startup, the state machine loads the latest snapshot and then replays any WAL entries that are newer than the snapshot, ensuring the in-memory index is fully up-to-date.
- **Snapshotting for Raft:** For Raft's own snapshotting purposes, the state machine creates a full backup of its underlying PebbleDB directory, zips it, and transfers it to the requesting replica.

### 4. gRPC Server

The worker exposes an internal gRPC API for other services (primarily the `apigateway`) to use.

- **Request Routing:** The gRPC server acts as a router. Every incoming request (e.g., `SearchRequest`) contains a `shard_id`. The handler uses this ID to direct the operation to the correct Raft group managed by the local `NodeHost`.
- **Consistency:** The server strictly distinguishes between reads and writes. All write RPCs are routed through `SyncPropose` to the Raft log. All read RPCs are routed through `SyncRead` to ensure linearizable consistency.

## Internal API

The `worker`'s internal gRPC API is defined in `proto/worker/worker.proto`. It provides RPCs for data and search operations, which are intended to be called by the `apigateway`.

- `StoreVector`: Inserts or updates a vector in a shard.
- `GetVector`: Retrieves a vector by its ID.
- `DeleteVector`: Deletes a vector by its ID.
- `Search`: Performs a k-NN similarity search within a shard.

## Configuration and Running

The worker is configured via command-line flags. It requires the addresses of the `placementdriver` cluster to register itself and begin receiving shard assignments.

**Example: Starting a Worker Node**

```bash
./bin/worker \
  -node-id 1 \
  -grpc-addr="localhost:9090" \
  -raft-addr="localhost:9191" \
  -pd-addrs="localhost:6001,localhost:6002,localhost:6003" \
  -data-dir="./worker-data"
```

Upon starting, the worker will:

1. Initialize its `NodeHost` and `ShardManager`.
2. Connect to a leader in the `placementdriver` cluster.
3. Register itself to get a unique worker ID.
4. Begin a heartbeat loop to report its status and receive shard assignments.
5. Dynamically start and stop shard replicas as instructed by the `placementdriver`.
