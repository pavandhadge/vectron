# Vectron Worker Service Documentation

This document provides a detailed analysis of the Vectron `worker` service.

## 1. Overview

The `worker` is the stateful workhorse of the Vectron cluster. It is a single service responsible for storing vector data, building and managing vector indexes, and executing search queries. The worker is designed to be a horizontally scalable, fault-tolerant data node.

### 1.1. Core Responsibilities

*   **Data Storage:** Persistently stores vector data and metadata using the PebbleDB key-value store.
*   **Vector Indexing:** Builds and maintains an in-memory HNSW index for fast approximate nearest neighbor (ANN) searches. The index is durably persisted using a snapshotting and WAL strategy.
*   **Search Execution:** Executes k-NN search queries on the vector data it owns.
*   **Data Sharding & Replication:** Manages multiple, independent, replicated data shards simultaneously using a "multi-Raft" architecture powered by the Dragonboat library.
*   **Communication with Placement Driver:** Registers itself with the `placementdriver`, reports its status via heartbeats, and dynamically starts or stops shard replicas based on commands from the PD.

## 2. Architecture

The worker's architecture is a sophisticated composition of a key-value store, a vector index, and a "multi-Raft" consensus system.

### 2.1. Multi-Raft Sharding with Dragonboat

A single worker node can host replicas for many different shards. Each shard is its own independent Raft consensus group.

*   **Shard Manager:** This component communicates with the `placementdriver` via a heartbeat. The PD's response tells the manager which shards the worker is expected to host. The manager's job is to reconcile this desired state, starting and stopping shard Raft groups as commanded.
*   **Data Replication:** All write operations for a shard (e.g., `StoreVector`) are submitted as commands to the shard's Raft log using a linearizable `SyncPropose`. The command is only applied after being replicated to a quorum of the shard's replicas, guaranteeing data durability and consistency.

### 2.2. Per-Shard State Machine (FSM)

Each shard (Raft group) has its own instance of a `StateMachine`. This is the core of the data layer.

*   **Composition:** The state machine is built directly on top of a storage engine.
*   **Write Path:** The `Update` method of the FSM is called by Dragonboat to apply a committed Raft log entry. It decodes the command and calls the corresponding write method on the underlying storage engine.
*   **Read Path:** The `Lookup` method is used for linearizable reads. The gRPC server uses Dragonboat's `SyncRead` to invoke this method, which in turn calls the appropriate read method on the storage engine (e.g., performing a vector search).

### 2.3. Storage and Indexing Engine

Each state machine instance manages its own dedicated storage on disk.

*   **Key-Value Store:** The worker uses **PebbleDB** (a RocksDB-inspired key-value store written in Go) as its underlying storage engine.
*   **Vector Index:** For efficient similarity search, the worker uses a **Hierarchical Navigable Small World (HNSW)** index via the `idxhnsw` package.
*   **Index Persistence:** The in-memory HNSW index is made durable using a two-pronged strategy:
    1.  **Snapshotting:** Periodically, the entire HNSW graph is serialized and saved as a single value within PebbleDB.
    2.  **Write-Ahead Log (WAL):** Every modification to the HNSW index is also written as a small entry into PebbleDB.
    On startup, the state machine loads the latest snapshot and then replays any WAL entries that are newer than the snapshot to ensure the in-memory index is fully up-to-date.

### 2.4. Raft Snapshotting

For Raft's own snapshotting needs (to bring slow replicas up to speed), the state machine creates a full backup of its underlying PebbleDB directory, zips it, and transfers it to the requesting replica.

### 2.5. gRPC Server

The worker exposes an internal gRPC API for other services to use.

*   **Request Routing:** The gRPC server acts as a router. Every incoming request contains a `shard_id`. The handler uses this ID to direct the operation to the correct Raft group.
*   **Consistency:** The server strictly distinguishes between reads and writes. All write RPCs are routed through `SyncPropose` to the Raft log. All read RPCs are routed through `SyncRead` to ensure linearizable consistency.

## 3. API Definition (from `shared/proto/worker/worker.proto`)

The `worker` exposes an internal gRPC API that is used by other services in the cluster, primarily the `apigateway`. This API is not intended for public consumption.

### 3.1. Analysis of the API

*   **Shard-Centric Design:** Every request message contains a `uint64 shard_id`. This is a critical design choice. It means the gRPC server on the worker acts as a top-level router, and the calling service (e.g., `apigateway`) is responsible for specifying which shard the operation is for.
*   **Vector and KV Operations:** The API provides high-level vector operations (`StoreVector`, `Search`, etc.) as well as a raw key-value interface (`Put`, `Get`, `Delete`). This suggests the underlying storage is a flexible KV store and that the vector functionality is built on top of it.
*   **Internal Data Structures:** The internal `Vector` message uses `bytes metadata`, confirming that the public-facing `map<string, string> payload` is serialized into a byte slice by the `translator` before being sent to the worker.
*   **Internal Options:** The `SearchRequest` includes internal-only options like `brute_force`, which are not exposed in the public API but can be used for debugging or special cases.

### 3.2. Proto Definition

```protobuf
syntax = "proto3";

package vectron.worker.v1;

option go_package = "github.com/pavandhadge/vectron/shared/proto/worker";

// The worker service definition.
service WorkerService {
  // Vector operations
  rpc StoreVector(StoreVectorRequest) returns (StoreVectorResponse) {}
  rpc GetVector(GetVectorRequest) returns (GetVectorResponse) {}
  rpc DeleteVector(DeleteVectorRequest) returns (DeleteVectorResponse) {}
  rpc Search(SearchRequest) returns (SearchResponse) {}

  // Key-value operations
  rpc Put(PutRequest) returns (PutResponse) {}
  rpc Get(GetRequest) returns (GetResponse) {}
  rpc Delete(DeleteRequest) returns (DeleteResponse) {}

  // Control/Admin operations
  rpc Status(StatusRequest) returns (StatusResponse) {}
  rpc Flush(FlushRequest) returns (FlushResponse) {}
}

// Vector messages
message Vector {
  string id = 1;
  repeated float vector = 2;
  bytes metadata = 3;
}

message StoreVectorRequest {
  uint64 shard_id = 1;
  Vector vector = 2;
}

message StoreVectorResponse {}

message GetVectorRequest {
  uint64 shard_id = 1;
  string id = 2;
}

message GetVectorResponse {
  Vector vector = 1;
}

message DeleteVectorRequest {
  uint64 shard_id = 1;
  string id = 2;
}

message DeleteVectorResponse {}

message SearchRequest {
  uint64 shard_id = 1;
  repeated float vector = 2;
  int32 k = 3;
  bool brute_force = 4;
}

message SearchResponse {
  repeated string ids = 1;
  repeated float scores = 2;
}

// Key-value messages
message KeyValuePair {
  bytes key = 1;
  bytes value = 2;
}

message PutRequest {
  uint64 shard_id = 1;
  KeyValuePair kv = 2;
}

message PutResponse {}

message GetRequest {
  uint64 shard_id = 1;
  bytes key = 2;
}

message GetResponse {
  KeyValuePair kv = 1;
}

message DeleteRequest {
  uint64 shard_id = 1;
  bytes key = 2;
}

message DeleteResponse {}

// Control/Admin messages
message StatusRequest {
  uint64 shard_id = 1;
}

message StatusResponse {
  string status = 1;
}

message FlushRequest {
  uint64 shard_id = 1;
}

message FlushResponse {}
```


## 4. Service Entrypoint (`cmd/worker/main.go`)

The `main.go` file is the master controller that bootstraps the entire worker node, initializing all its components and starting its main control loops.

### 4.1. Configuration via Command-Line Flags

Unlike the other services, the worker is configured via command-line flags:

| Flag          | Description                                           | Default             |
| ------------- | ----------------------------------------------------- | ------------------- |
| `-grpc-addr`  | The address for the internal gRPC server to listen on.  | `localhost:9090`    |
| `-raft-addr`  | The address for the Raft `NodeHost` to communicate on.    | `localhost:9191`    |
| `-pd-addrs`   | A comma-separated list of `placementdriver` addresses.    | `localhost:6001`    |
| `-node-id`    | A unique, positive integer ID for this worker node.   | `1`                 |
| `-data-dir`   | The parent directory for all persistent worker data.      | `./worker-data`     |

### 4.2. `Start` Function: The Initialization Sequence

The `Start` function orchestrates the intricate startup process:

1.  **Data Directory Setup:** It creates a dedicated data directory for the Raft `NodeHost` within the main `data-dir` (e.g., `./worker-data/node-1/`), isolating the Raft data for each worker.

2.  **Dragonboat `NodeHost` Initialization:** It configures and creates a `dragonboat.NodeHost`. This `NodeHost` is the core of the replication system; it is a long-lived object that manages all Raft groups (shards) that will run on this worker.

3.  **`ShardManager` Creation:** It instantiates the `shard.Manager`, passing it the `NodeHost`. The `ShardManager`'s purpose is to abstract away the starting and stopping of the individual Raft groups.

4.  **`placementdriver` Client Creation:** It creates the `pd.Client`, which is responsible for all communication with the `placementdriver` cluster.

5.  **Registration with Placement Driver:** It calls `pdClient.Register()` in a retry loop. This is the worker announcing its presence, address, and `nodeID` to the `placementdriver`. This step is critical for the PD to know that the worker is available to be assigned shards.

6.  **Starting the Main Control Loops:** Two crucial, long-running goroutines are launched, forming the heart of the worker's dynamic nature:
    *   **Heartbeat Loop:** `pdClient.StartHeartbeatLoop()` begins sending periodic heartbeats to the `placementdriver`. The PD's response to each heartbeat contains a list of shards that this worker should be hosting. These assignments are passed into a channel.
    *   **Shard Sync Loop:** A second goroutine listens on the channel for shard assignments from the heartbeat loop. When it receives a new list, it calls `shardManager.SyncShards()`. This is the **reconciliation loop**, where the `ShardManager` compares the desired state from the PD with its current state and starts or stops shard Raft groups to match.

7.  **gRPC Server Startup:** Finally, it starts the worker's internal gRPC server. It instantiates the `internal.GrpcServer` implementation, crucially passing it the `NodeHost` and `ShardManager`. This gives the gRPC handlers the handles they need to propose commands to the correct shard's Raft log or to perform reads.

### 4.3. Graceful Shutdown

The `main` function listens for `SIGINT` and `SIGTERM` signals. When a signal is received, it logs a shutdown message. The code notes that a full graceful shutdown sequence (stopping the `NodeHost`, etc.) would be required for a production-ready implementation.


## 5. gRPC Handlers (`internal/grpc.go`)

The `grpc.go` file implements the `worker.WorkerServiceServer` interface. It is the API layer of the worker, acting as the bridge between incoming gRPC requests and the underlying Raft consensus system.

### 5.1. `GrpcServer` Struct

The `GrpcServer` struct holds pointers to the two most important components of the worker:

```go
type GrpcServer struct {
	worker.UnimplementedWorkerServiceServer
	nodeHost     *dragonboat.NodeHost
	shardManager *shard.Manager
}
```

*   `nodeHost`: The handle to the Dragonboat `NodeHost`, which is used for all direct interactions with the Raft consensus groups (proposing writes and performing reads).
*   `shardManager`: Used to check the status and readiness of a shard before attempting to perform an operation on it.

### 5.2. Read/Write Path Separation (CQRS)

The handlers demonstrate a clear and strict implementation of Command Query Responsibility Segregation (CQRS) as it applies to a consensus system. Reads and writes are handled through completely different paths to guarantee data consistency.

#### 5.2.1. The Write Path (`StoreVector`, `DeleteVector`)

All operations that modify state follow the same "propose to Raft" pattern:

1.  **Readiness Check:** The handler first calls `s.shardManager.IsShardReady()` to ensure the target shard is active and ready to accept writes on this node. This prevents errors if the shard is being moved or shut down.
2.  **Command Creation:** A `shard.Command` struct is created. This struct contains a `Type` (e.g., `shard.StoreVector`) and the data for the operation. This is the canonical data structure that the shard's State Machine understands.
3.  **Serialization:** The `shard.Command` is marshaled into a JSON byte slice (`[]byte`). This byte slice is the data that will be replicated across the Raft group.
4.  **Proposal:** The handler calls `s.nodeHost.SyncPropose()`. This is a blocking call that submits the serialized command to the Raft log. The function **only returns after the command has been replicated to a quorum of the shard's replicas, committed to the log, and applied to the state machine on the leader node.** This process guarantees **linearizability** for all write operations.

#### 5.2.2. The Read Path (`Search`, `GetVector`)

All operations that only read state follow the "linearizable read" pattern:

1.  **Query Creation:** A query struct is created (e.g., `shard.SearchQuery`). This struct contains the parameters for the read operation. It is **not** a command and is **not** written to the Raft log.
2.  **Linearizable Read:** The handler calls `s.nodeHost.SyncRead()`. This is a blocking call that guarantees a **linearizable read**. Dragonboat ensures that the read is executed against a version of the state machine that is at least as up-to-date as the last committed entry in the Raft log, preventing stale reads.
3.  **State Machine Interaction:** The `SyncRead` call passes the query struct directly to the `Lookup` method of the appropriate shard's State Machine, which performs the actual data retrieval or search.
4.  **Return and Decode:** The result from the `Lookup` method is returned by `SyncRead`. The gRPC handler then type-asserts this result into the expected struct (e.g., `*shard.SearchResult`), translates it into the gRPC response format, and returns it to the client.

### 5.3. Unimplemented Methods

The proto file defines key-value (`Put`, `Get`, `Delete`) and administrative (`Status`, `Flush`) RPCs. The gRPC handlers for these methods are present, but they simply return an `Unimplemented` error code. This indicates that while the API is designed to support these operations, the actual implementation is focused solely on the vector database functionality.


## 6. Raft and Replication (`internal/raft.go` and `internal/shard/`)

The worker's fault tolerance and data consistency are built on top of the Raft consensus algorithm, managed by the Dragonboat library. Each worker node is a "multi-Raft" node, capable of hosting many independent Raft consensus groups, where each group corresponds to a single data shard.

### 6.1. Raft Event Listener (`raft.go`)

The `internal/raft.go` file defines a `loggingEventListener`. This is a struct that implements Dragonboat's `RaftEventListener` interface. Its purpose is purely for observability and debugging. It hooks into the lifecycle events of the Raft `NodeHost` (e.g., `LeaderUpdated`, `SnapshotCreated`, `MembershipChanged`) and prints a formatted log message for each one, providing a real-time view into the inner workings of the consensus system.

### 6.2. `ShardManager`: The Reconciliation Controller (`shard/manager.go`)

The `ShardManager` is the brain of the worker node, responsible for managing the lifecycle of all shard replicas. It acts as a classic reconciliation controller, constantly working to make the worker's local state match the desired state dictated by the `placementdriver`.

*   **Reconciliation Loop (`SyncShards`):** This is the core function of the manager. It's called periodically when the worker receives an updated list of shard assignments from the `placementdriver`.
    1.  **State Comparison:** It compares the list of desired shards from the PD with its internal map of currently running shards.
    2.  **Stop Old Replicas:** If a locally running shard is no longer in the desired state (e.g., it was moved to another worker), the manager calls `nodeHost.StopCluster()` to terminate that Raft group.
    3.  **Start New Replicas:** If a desired shard is not yet running locally, the manager calls `nodeHost.StartOnDiskCluster()`. This critical call instructs Dragonboat to create and start a new Raft consensus group.

*   **State Machine Factory:** When starting a new shard, the `ShardManager` provides Dragonboat with a factory function (`createFSM`). This function is responsible for creating a new, dedicated instance of the `StateMachine` for that shard, configured with the correct database path, vector dimension, and distance metric.

*   **Readiness Check (`IsShardReady`):** The manager provides a crucial `IsShardReady` method used by the gRPC server. It checks if a shard is both actively running and has a healthy Raft group with an elected leader. This prevents operations from being attempted on a shard that is still bootstrapping, has lost its quorum, or is being decommissioned.

### 6.3. `StateMachine`: The Heart of the Shard (`shard/state_machine.go`)

The `StateMachine` is the most critical component of the data layer. Each shard has its own independent instance. It implements Dragonboat's `IOnDiskStateMachine` interface, which connects it directly to the Raft log.

*   **Composition:** The `StateMachine` struct embeds the `*storage.PebbleDB` client. This is a powerful Go pattern that means the `StateMachine` *is* the storage engine for that shard. It can directly call methods like `StoreVector` and `Search` on itself.

*   **The Write Path (`Update`):** This method is called by Dragonboat *after* a command has been successfully replicated and committed to the Raft log.
    1.  It receives a batch of serialized commands from the log.
    2.  It unmarshals each command into a `shard.Command` struct.
    3.  Using a `switch` statement on the command type (`StoreVector`, `DeleteVector`), it calls the appropriate write method on the embedded `PebbleDB` instance.
    This guarantees that all writes are applied deterministically and in the same order on all replicas, ensuring they remain identical.

*   **The Read Path (`Lookup`):** This method is called by Dragonboat when a `SyncRead` is requested by a gRPC handler.
    1.  It receives a query object (e.g., `SearchQuery`).
    2.  It uses a type switch to identify the query type.
    3.  It calls the appropriate read method on the embedded `PebbleDB` instance (e.g., `Search`).
    This provides linearizable reads, ensuring clients always see the most up-to-date committed data.

*   **Snapshotting (`SaveSnapshot` & `RecoverFromSnapshot`):** The state machine implements robust, file-based snapshotting to bring new or lagging replicas up to speed efficiently.
    *   `SaveSnapshot`: When triggered by Dragonboat, this method calls the underlying storage engine's `Backup()` function to create a consistent backup of the entire PebbleDB database on disk. It then `zip`s this backup directory and streams it to Dragonboat, which transmits it to the destination replica.
    *   `RecoverFromSnapshot`: On the receiving replica, this method gets the zipped snapshot stream, unzips it to a temporary directory, and calls the storage engine's `Restore()` function to replace its local database with the contents of the snapshot.

## 7. Storage Engine (`internal/storage/`)

The storage engine is the lowest level of the data plane, responsible for the physical persistence of all shard data, including both the raw vector data and the HNSW index. The implementation uses **PebbleDB**, a high-performance key-value store inspired by RocksDB.

### 7.1. The `PebbleDB` Struct

This struct is the concrete implementation of the `Storage` interface. It's a composite object that owns and manages both the on-disk PebbleDB instance and the in-memory HNSW index.

```go
type PebbleDB struct {
	db        *pebble.DB
	writeOpts *pebble.WriteOptions
	hnsw      *idxhnsw.HNSW // The HNSW index
	opts      *Options
	stop      chan struct{}
	wg        sync.WaitGroup
}
```

*   `db`: The handle to the PebbleDB instance.
*   `hnsw`: A pointer to the in-memory `HNSW` index for this shard.
*   `stop`, `wg`: Used to manage a background goroutine for periodic index snapshotting.

### 7.2. Initialization and Index Recovery

The `Init` method orchestrates the startup of the storage engine. Its most critical task is to load the HNSW index into memory and ensure it's up-to-date.

1.  **DB Open:** It opens the PebbleDB database at the specified path.
2.  **Load HNSW:** It calls a helper, `loadHNSW`, which implements the core recovery logic:
    *   **Load Snapshot:** It first attempts to read the key `_hnsw_index` from PebbleDB. This key contains the last full snapshot of the serialized HNSW graph. It loads this snapshot into a new in-memory `HNSW` instance.
    *   **Replay WAL:** If the snapshot is loaded successfully, it then calls `replayWAL`. This method reads the timestamp of the last snapshot from the `_hnsw_index_ts` key. It then iterates through all keys with the prefix `_hnsw_wal_` that are *newer* than the snapshot's timestamp. For each WAL entry, it applies the corresponding operation (`Add` or `Delete`) to the in-memory HNSW index.

This **load-snapshot-then-replay-log** strategy is a classic database recovery technique that provides fast startup times, as it avoids having to rebuild the entire HNSW index from scratch by re-reading every vector in the database.

### 7.3. HNSW Index Persistence Strategy

The storage engine uses a robust two-pronged strategy to ensure the in-memory HNSW index is durable.

1.  **Write-Ahead Log (WAL):**
    *   The `StoreVector` and `DeleteVector` methods use `pebble.Batch` to perform an **atomic write**.
    *   When storing a vector, the batch contains two `Set` operations: one for the vector data itself (`v_<id>`) and one for the WAL entry (`_hnsw_wal_<ts>_<id>`).
    *   When deleting, the batch contains a `Delete` for the vector data and a `Set` for a WAL "tombstone" (`_hnsw_wal_<ts>_<id>_delete`).
    *   Because the write is atomic, the WAL entry is guaranteed to be present if and only if the data operation was also committed.
2.  **Snapshotting (`saveHNSW`):**
    *   A background goroutine (`persistenceLoop`) calls `saveHNSW` periodically (e.g., every 5 minutes).
    *   `saveHNSW` serializes the *entire* current in-memory HNSW graph into a byte slice.
    *   It writes this new snapshot to the `_hnsw_index` key.
    *   Crucially, after writing the new snapshot, it deletes all WAL entries that were created before the snapshot's timestamp, effectively compacting the log.

### 7.4. The Write Path (`StoreVector`)

When `StoreVector` is called by the `StateMachine`:
1.  It first adds the vector to the in-memory `HNSW` index.
2.  It then creates an atomic `pebble.Batch` to write both the vector data (key `v_<id>`) and the WAL entry (key `_hnsw_wal_...`) to PebbleDB.
3.  If any part of the persistent write fails, it attempts to roll back the in-memory HNSW `Add`.

### 7.5. The Read Path (`Search` and `GetVector`)

*   `Search`: This method delegates directly to the in-memory `hnsw.Search()`. This is extremely fast as it does not need to touch the disk. It then filters the results to ensure the vectors have not been hard-deleted from PebbleDB.
*   `GetVector`: This method reads directly from PebbleDB by key (`v_<id>`) and then calls `decodeVectorWithMeta` to deserialize the on-disk format into the vector and metadata components.

## 8. HNSW Index (`internal/idxhnsw/`)

The `idxhnsw` package implements the Hierarchical Navigable Small World (HNSW) algorithm for efficient approximate nearest neighbor (ANN) search. This is the core component that enables fast similarity queries on the vector data.

### 8.1. `HNSW` Core Structure (`hnsw.go`)

The `HNSW` struct represents the entire index.

*   **`HNSWConfig`:** Defines critical parameters of the algorithm:
    *   `M`: Maximum number of connections per node per layer (influences connectivity and search path length).
    *   `EfConstruction`: Size of the dynamic candidate list during graph construction (higher = better quality, slower construction).
    *   `EfSearch`: Size of the dynamic candidate list during search (higher = better accuracy, slower search).
    *   `MaxLevel`: The maximum layer a node can be part of.
    *   `Distance`: The distance metric used (Euclidean or Cosine).
*   **Graph Representation:**
    *   `nodes`: A `map[uint32]*Node` storing the actual graph nodes, where `uint32` are internal IDs.
    *   `entry`: The internal ID of the current entry point node (the starting point for searches).
    *   `maxLayer`: The highest layer currently existing in the graph.
*   **ID Mapping:** Manages the translation between external `string` IDs and internal `uint32` IDs for efficiency.
*   **Concurrency:** Protected by a `sync.RWMutex` to allow concurrent reads and serialized writes.
*   **`NodeStore` Interface:** The `HNSW` struct relies on a `NodeStore` interface for persistence. This decouples the HNSW algorithm from the underlying storage mechanism (PebbleDB), allowing the graph nodes themselves to be stored durably. This means individual nodes are likely serialized and stored as key-value pairs in PebbleDB.
*   **Soft Deletion:** The implementation includes fields (`deletedCount`) and a background cleanup process (`StartCleanup`) for managing soft-deleted nodes.
*   **Serialization:** The `gob` package is used to serialize and deserialize the entire `HNSW` struct for snapshotting purposes, storing the full index state in PebbleDB.

### 8.2. `Add` Operation (`insert.go`)

The `Add` method implements the HNSW graph construction algorithm.

1.  **ID Management:** Maps the external `string` ID to an internal `uint32` ID. If an ID already exists, it's treated as an update (delete then add).
2.  **Random Layer Assignment:** A `layer` is probabilistically assigned to the new node using an exponentially decaying distribution (`randomLayer`). This ensures the multi-layered structure of HNSW.
3.  **Node Persistence:** The new node's data is persisted via the `NodeStore` interface *before* it is fully connected into the in-memory graph.
4.  **Greedy Search for Entry Point:** Starting from the current global entry point at `maxLayer`, it performs a greedy search (`searchLayerSingle`) down to the new node's assigned layer. This efficiently finds a good starting point for connections.
5.  **Iterative Insertion (Layer by Layer):**
    *   From the new node's layer down to layer 0, the algorithm connects the new node to its neighbors.
    *   For each layer, it first performs an expanded search (`searchLayer`) to find `EfConstruction` candidate neighbors.
    *   **Neighbor Selection Heuristic (`selectNeighborsHeuristic`):** From these candidates, the algorithm selects `M` neighbors. This heuristic prioritizes close neighbors but also includes a diversity mechanism to avoid overly dense clusters, improving graph quality.
    *   **Bidirectional Connections & Pruning:** The new node is connected to its selected neighbors, and conversely, its neighbors are connected back to the new node. If a neighbor exceeds `M` connections, its neighbor list is pruned using the same heuristic.
6.  **Global Entry Point Update:** If the new node is inserted into a layer higher than the current `maxLayer`, it becomes the new global entry point.

### 8.3. `Search` Operation (`search.go`)

The `Search` method implements the efficient ANN query algorithm.

1.  **Top-Down Greedy Traversal:** The search starts at the global entry point (`h.entry`) at the highest layer (`h.maxLayer`). It then performs a greedy search (`searchLayerSingle`) iteratively down to layer 1. This quickly narrows the search space to a promising region.
2.  **Expanded Search at Base Layer (`searchLayer`):** At layer 0 (the base layer, where all nodes exist), a more thorough expanded search is performed.
    *   It uses two priority queues: a min-heap (`candidates`) for nodes to explore, and a max-heap (`results`) to maintain the `EfSearch` best nodes found so far.
    *   The algorithm explores neighbors of promising candidates, continually updating the `results` heap to keep track of the closest `EfSearch` nodes.
3.  **Result Refinement:** The final candidates from `searchLayer` are sorted by distance, and the top `k` results are returned.
4.  **ID Conversion:** Internal `uint32` IDs are converted back to external `string` IDs for the final output.

## 9. Placement Driver Client (`internal/pd/`)

The `internal/pd` package implements the client-side logic for the worker to communicate with the `placementdriver` service. This communication is essential for the worker's operational lifecycle, including registration, status reporting, and receiving instructions for shard management.

### 9.1. `Client` Struct

The `Client` struct encapsulates the worker's connection and interaction with the `placementdriver`:

```go
type Client struct {
	pdAddrs      []string
	leader       *LeaderInfo
	leaderMu     sync.RWMutex
	workerID     uint64
	grpcAddr     string
	raftAddr     string
	shardManager ShardManager
}
```

*   `pdAddrs`: A list of known `placementdriver` gRPC addresses.
*   `leader`, `leaderMu`: Stores the gRPC client and connection to the currently identified `placementdriver` leader, protected by a mutex for concurrent access.
*   `workerID`: The unique ID assigned to this worker by the `placementdriver` during registration.
*   `grpcAddr`, `raftAddr`: The network addresses where this worker's gRPC and Raft communication endpoints are listening.
*   `shardManager`: An interface to the worker's `ShardManager`, used to query the local status of shards to include in heartbeats.

### 9.2. Leader Discovery (`updateLeader`)

Similar to the `apigateway`, the `pd.Client` implements a robust leader discovery mechanism for the `placementdriver` cluster. The `updateLeader` method iterates through the configured `pdAddrs`, attempting to connect to each one. It sends a test RPC (`ListWorkers`) to identify the current leader. This ensures the worker can remain connected to the `placementdriver` even if the leader changes.

### 9.3. Worker Registration (`Register`)

This is the initial handshake.
1.  The worker sends its `grpcAddr` and `raftAddr` to the `placementdriver` leader via the `RegisterWorker` RPC.
2.  The `placementdriver` assigns a `WorkerId` to this worker.
3.  The client parses this `WorkerId` and stores it internally, using it for all subsequent communications.
4.  The method includes retry logic and leader re-discovery to handle transient failures during registration.

### 9.4. Heartbeat Loop (`StartHeartbeatLoop` and `sendHeartbeat`)

The `StartHeartbeatLoop` runs a background goroutine that periodically calls `sendHeartbeat` (every 5 seconds). This continuous communication is vital for the control plane.

*   **Status Reporting:** In `sendHeartbeat`, the worker calls `c.shardManager.GetShardLeaderInfo()` to gather the current state of all shards it hosts (their IDs and their Raft leader IDs). This information is sent to the `placementdriver`.
*   **Leader Resilience:** The `sendHeartbeat` method incorporates extensive retry and leader re-discovery logic to handle scenarios where the current `placementdriver` leader becomes unresponsive.
*   **Shard Assignments:** The `placementdriver`'s response to a heartbeat can include a JSON-encoded message containing a list of `ShardAssignment`s. These assignments are unmarshaled and sent over a channel (`shardUpdateChan`) to the `ShardManager`, which then processes them to synchronize the worker's local state.

### 9.5. Data Structures for Shard Assignments (`types.go`)

The `worker/internal/pd/types.go` file defines the data structures used for communication between the `placementdriver` and the worker regarding shard management:

*   **`ShardAssignment`:** The main instruction sent from the `placementdriver` to the worker.
    *   `ShardInfo`: Contains detailed metadata about the shard.
    *   `InitialMembers`: A map of `nodeID` to Raft address for all replicas in the shard's Raft group. This is used by `ShardManager.StartOnDiskCluster` to initiate a Raft cluster.
*   **`ShardInfo`:** Holds all metadata for a specific shard.
    *   `ShardID`: Unique ID for the shard.
    *   `Collection`: Name of the collection the shard belongs to.
    *   `KeyRangeStart`, `KeyRangeEnd`: Defines the hash range this shard is responsible for.
    *   `Replicas`: List of worker `nodeID`s expected to host replicas.
    *   `LeaderID`: The ID of the current Raft leader for the shard.
    *   `Dimension`, `Distance`: Vector parameters for the shard's HNSW index.

These data structures form the contract that enables the `placementdriver` to dynamically manage and orchestrate the distributed state of shards across the worker cluster.
