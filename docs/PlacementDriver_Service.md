# Vectron Placement Driver Service Documentation

This document provides a detailed analysis of the Vectron `placementdriver` service.

## 1. Overview

The `placementdriver` service is the central coordinator for the entire Vectron distributed vector database. It is a fault-tolerant service responsible for managing the cluster topology, including registered workers, defined collections, and the assignment of shards to worker nodes. It acts as the source of truth for all cluster metadata.

### 1.1. Core Responsibilities

*   **Worker Management:** Registers new worker nodes and monitors their liveness through heartbeats.
*   **Collection Management:** Manages the creation, deletion, and metadata of vector collections.
*   **Shard Management:** Divides collections into shards and assigns these shards (including their replicas) to available worker nodes.
*   **Service Discovery:** Provides information to the `apigateway` and workers about which worker node is responsible for a given shard or vector ID.
*   **Fault Tolerance:** Uses the Raft consensus protocol to ensure its own state is replicated and consistent, making it resilient to failures.

## 2. Architecture

The `placementdriver` service is built on a foundation of the Raft consensus protocol for fault tolerance and consistency. Its architecture is composed of three main internal components: the Raft layer, the Finite State Machine (FSM), and the gRPC server.

### 2.1. Raft Layer (`internal/raft`)

*   The service uses the `dragonboat/v3` library to implement the Raft consensus protocol.
*   All state-changing operations are submitted as proposals to the Raft log.
*   This ensures that commands are replicated across a majority of `placementdriver` nodes before being applied, guaranteeing consistency and durability.

### 2.2. State Management (`internal/fsm`)

*   The core of the `placementdriver`'s logic resides in its deterministic Finite State Machine (FSM).
*   The FSM holds the entire cluster state in memory: a list of registered workers, definitions of all collections, and the current assignment of shards to workers.
*   The FSM's `Update` method processes commands from the Raft log (e.g., `CreateCollection`, `RegisterWorker`) and applies them to its in-memory state.
*   The state is periodically snapshotted to disk for faster recovery and log compaction.
*   The FSM is also responsible for the logic of shard assignment (e.g., round-robin distribution).

### 2.3. API Layer (`internal/server`)

*   A gRPC server exposes the `PlacementService` API.
*   **Write Operations:** Client requests that modify the cluster state (e.g., `CreateCollection`, `RegisterWorker`) are translated into commands that are proposed to the Raft cluster through the `internal/raft` layer.
*   **Read Operations:** Client requests that only query the cluster state (e.g., `ListWorkers`, `GetWorker`) are served directly from the local FSM's in-memory state for high performance and low latency. These reads are linearizable, meaning they reflect the latest committed state.

### 2.4. Key Processes

*   **Worker Registration:** Workers register with the `placementdriver` on startup via the `RegisterWorker` RPC, reporting their network addresses.
*   **Worker Heartbeats:** Registered workers periodically send `Heartbeat` RPCs to signal their liveness and report the status of the shards they host.
*   **Shard Assignment:** When a collection is created, the `placementdriver` divides a `uint64` hash space into shards. It then assigns replicas of these shards to available workers, typically using a round-robin strategy for initial distribution.
*   **Service Discovery:** The `apigateway` and workers use the `GetWorker` RPC to find the correct worker address for a given vector operation. The `placementdriver` uses consistent hashing to map a vector ID to a specific shard and returns the address of a worker hosting that shard.
*   **Leader Election:** Leader election for the `placementdriver` cluster itself is handled automatically by the Dragonboat Raft library. The `placementdriver` also tracks the leader status of the individual data shards (which are Raft clusters on the workers) reported via worker heartbeats.

## 3. API Definition (from `shared/proto/placementdriver/placementdriver.proto`)

The `placementdriver` exposes a gRPC API that serves as the central control plane for the Vectron cluster. It is used by workers (for registration and heartbeats) and the API Gateway (for service discovery and collection management).

### 3.1. Analysis of the API

The API can be categorized into three main areas:

1.  **Worker Management:**
    *   `RegisterWorker`: Workers use this to register themselves with the `placementdriver`. The `placementdriver` assigns a unique `worker_id`.
    *   `Heartbeat`: Workers send periodic heartbeats to report their liveness and local shard status. The `placementdriver` can respond with shard assignments.
    *   `ListWorkers`: For monitoring purposes, allowing clients to see all registered workers.
2.  **Collection Management:**
    *   `CreateCollection`, `DeleteCollection`, `ListCollections`, `GetCollectionStatus`: These RPCs allow for the lifecycle management and querying of vector collections.
3.  **Service Discovery & Sharding:**
    *   `GetWorker`: A crucial RPC used by the `apigateway` to determine which worker (and shard) is responsible for a given collection and (optionally) `vector_id`. This enables consistent hashing.
4.  **Admin/Internal:**
    *   `Rebalance`: A placeholder for future rebalancing functionality.
    *   `GetLeader`: An RPC to determine the current `placementdriver` leader's address.

### 3.2. Proto Definition

```protobuf
syntax = "proto3";

package vectron.placementdriver.v1;

option go_package = "github.com/pavandhadge/vectron/shared/proto/placementdriver";
// ===============================================
// Placement Driver — The Brain of Vectron
// ===============================================

service PlacementService {
  // Get worker address for a collection + optional vector ID (for consistent hashing)
  rpc GetWorker(GetWorkerRequest) returns (GetWorkerResponse) {}

  // Worker registers itself on startup
  rpc RegisterWorker(RegisterWorkerRequest) returns (RegisterWorkerResponse) {}

  // Worker sends heartbeat (every 5s)
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse) {}

  // List all workers (for monitoring)
  rpc ListWorkers(ListWorkersRequest) returns (ListWorkersResponse) {}

  // Admin: force rebalance (future)
  rpc Rebalance(RebalanceRequest) returns (RebalanceResponse) {}
  // Collection management
  rpc CreateCollection(CreateCollectionRequest) returns (CreateCollectionResponse) {}
  rpc ListCollections(ListCollectionsRequest) returns (ListCollectionsResponse) {}
  rpc DeleteCollection(DeleteCollectionRequest) returns (DeleteCollectionResponse) {}
  rpc GetCollectionStatus(GetCollectionStatusRequest) returns (GetCollectionStatusResponse) {}
  rpc GetLeader(GetLeaderRequest) returns (GetLeaderResponse) {}
}

// ===============================================
// Messages
// ===============================================

message GetLeaderRequest {}

message GetLeaderResponse {
  string leader_address = 1;
}

message GetWorkerRequest {
  // Collection name (required)
  string collection = 1;

  // Optional: vector ID for consistent hashing
  // If empty → use collection-level sharding
  string vector_id = 2;
}

message GetWorkerResponse {
  // Full address: "10.0.0.42:6201" or "worker-3.internal:6201"
  string grpc_address = 1;

  // Optional: shard info
  uint32 shard_id = 2;
}

message RegisterWorkerRequest {
  string grpc_address = 1;
  string raft_address = 2;
  repeated string capabilities = 3; // ["gpu", "large-memory"]
  int64 timestamp = 4;
}

message RegisterWorkerResponse {
  string worker_id = 1; // assigned by placement driver

  bool success = 2;
}

message ShardLeaderInfo {
  uint64 shard_id = 1;

  uint64 leader_id = 2;
}

message HeartbeatRequest {
  string worker_id = 1;

  int64 timestamp = 2;

  // Optional metrics

  int64 vector_count = 3;

  int64 memory_bytes = 4;

  repeated ShardLeaderInfo shard_leader_info = 5;
}

message HeartbeatResponse {
  // If false → worker is considered dead
  bool ok = 1;
  // Optional: config updates
  string message = 2;
}

message ListWorkersRequest {}

message ListWorkersResponse {
  repeated WorkerInfo workers = 1;
}

message WorkerInfo {
  string worker_id = 1;
  string grpc_address = 2;
  string raft_address = 3;
  repeated string collections = 4;
  int64 last_heartbeat = 5;
  bool healthy = 6;
  map<string, string> metadata = 7;
}

message RebalanceRequest {
  // Future use
}

message RebalanceResponse {
  bool started = 1;
}

message CreateCollectionRequest {
  string name = 1;
  int32 dimension = 2;
  string distance = 3;
}

message CreateCollectionResponse {
  bool success = 1;
}

message ListCollectionsRequest {}

message ListCollectionsResponse {
  repeated string collections = 1;
}

message DeleteCollectionRequest {
  string name = 1;
}

message DeleteCollectionResponse {
  bool success = 1;
}

message GetCollectionStatusRequest {
  string name = 1;
}

message GetCollectionStatusResponse {
  string name = 1;
  int32 dimension = 2;
  string distance = 3;
  repeated ShardStatus shards = 4;
}

message ShardStatus {
  uint32 shard_id = 1;
  repeated uint64 replicas = 2;
  uint64 leader_id = 3;
  bool ready = 4;
}
```


## 4. Service Entrypoint (`cmd/placementdriver/main.go`)

The `main.go` file is the orchestrator for a single `placementdriver` node. It is responsible for parsing configuration, initializing the Raft consensus layer, setting up the Finite State Machine (FSM), and starting the gRPC server that exposes the `PlacementService` API.

### 4.1. Configuration via Command-Line Flags

The `placementdriver` uses command-line flags for its configuration:

| Flag              | Description                                                                 | Default             |
| ----------------- | --------------------------------------------------------------------------- | ------------------- |
| `-grpc-addr`      | The address for the gRPC server to listen on.                               | `localhost:6001`    |
| `-raft-addr`      | The address for the Raft communication among `placementdriver` nodes.       | `localhost:7001`    |
| `-node-id`        | This node's unique ID within the Raft cluster (must be > 0).                  | `1`                 |
| `-cluster-id`     | The unique ID for the entire `placementdriver` Raft cluster (e.g., `1`).      | `1`                 |
| `-initial-members`| A comma-separated list of initial Raft cluster members in `id:host:port` format. Crucial for bootstrapping. | `1:localhost:7001`  |
| `-data-dir`       | The directory to store Raft and FSM persistent data.                        | `pd-data`           |

The `parseInitialMembers` helper function is used to parse the `initial-members` string into a map, which is then used to configure the Raft node.

### 4.2. `Start` Function: The Core Initialization Sequence

The `Start` function is where all core components are brought to life:

1.  **Data Directory Setup:** It creates a dedicated directory (e.g., `pd-data/node-1`) for this specific `placementdriver` node's Raft data.
2.  **Raft Node Initialization:**
    *   It constructs a `pdRaft.Config` using the provided flags.
    *   It calls `pdRaft.NewNode(raftConfig)` to create and start the underlying Raft node. This `pdRaft.Node` encapsulates the Dragonboat library, providing the `placementdriver`'s own fault-tolerance and consistency. This is the most crucial step, bringing the consensus layer online.
3.  **FSM Retrieval:** It retrieves the initialized `FSM` (Finite State Machine) instance by calling `raftNode.GetFSM()`. This FSM will hold the entire cluster state (workers, collections, shards) and is where all application-level state changes are applied after being committed by Raft.
4.  **gRPC Server Creation:**
    *   It creates a new `server.Server` instance from the `internal/server` package, passing it references to both the `raftNode` (for proposing commands) and the `fsm` (for serving read queries).
    *   A standard gRPC server (`google.golang.org/grpc.NewServer()`) is then created.
    *   The `grpcServer` is registered with the `PlacementService` definition (`pb.RegisterPlacementServiceServer`).
    *   Finally, it starts listening on the configured `grpc-addr` and serving incoming requests.

### 4.3. Graceful Shutdown (Partial)

The `main` function starts the `Start` function in a goroutine and then enters a blocking state, waiting for `SIGINT` or `SIGTERM` signals. Upon receiving a signal, it logs a shutdown message and exits. A production-ready implementation would include explicit calls to gracefully stop the Raft node and gRPC server.


## 5. Raft Implementation (`internal/raft/raft.go`)

This section will explain how the `placementdriver` utilizes Dragonboat for its own Raft cluster.

## 6. Finite State Machine (FSM) (`internal/fsm/fsm.go`)

This section will describe the core logic for managing workers, collections, and shards within the FSM.

## 7. gRPC Server Implementation (`internal/server/server.go`)

The `internal/server/server.go` file implements the `PlacementService` gRPC API, which is the public interface for other services (workers and API Gateway) to interact with the `placementdriver`.

### 7.1. `Server` Struct

The `Server` struct is the concrete implementation of the `PlacementService` gRPC server.

```go
type Server struct {
	pb.UnimplementedPlacementServiceServer
	raft *raft.Node // The underlying Raft node for proposing state changes.
	fsm  *fsm.FSM   // A direct reference to the FSM for read-only operations.
}
```

*   `raft`: A reference to the `raft.Node` instance. This is used to propose commands to the Raft cluster, ensuring all state-changing operations are replicated and consistent.
*   `fsm`: A direct reference to the `fsm.FSM` instance. This allows the server to perform read-only queries directly against the local FSM's in-memory state.

### 7.2. Write RPCs (via Raft Proposals)

RPCs that modify the cluster state (e.g., `RegisterWorker`, `CreateCollection`) follow a consistent pattern:

1.  **Validation:** Basic validation of input parameters.
2.  **Payload & Command Encapsulation:** The request data is converted into a specific `fsm.Payload` struct (e.g., `fsm.RegisterWorkerPayload`), which is then marshaled into JSON. This JSON payload is wrapped within a generic `fsm.Command` struct, along with its `fsm.CommandType`.
3.  **Raft Proposal:** The `fsm.Command` is then marshaled into JSON and proposed to the Raft cluster using `s.raft.Propose()`. This is a blocking call that ensures the command is replicated to a quorum of `placementdriver` nodes and applied to the FSM before the RPC call returns. This guarantees linearizable consistency for writes.
4.  **Result Handling:** The result from the FSM's `Update` method (passed back via `raft.Propose`) is used to construct the RPC response.

#### `RegisterWorker`
Handles requests from new worker nodes. It proposes a `RegisterWorker` command, and the FSM assigns a new `worker_id`.

#### `CreateCollection`
Handles requests to create a new vector collection. It proposes a `CreateCollection` command. The FSM handles the logic of initial shard creation and assignment.

#### `Heartbeat` (Write Component)
Although primarily a read/response RPC, `Heartbeat` also contains write components:
*   It proposes an `UpdateWorkerHeartbeat` command to update the worker's last seen timestamp in the FSM.
*   It proposes `UpdateShardLeader` commands to update the leader information for shards reported by the worker.

### 7.3. Read RPCs (Direct FSM Queries)

RPCs that only query the cluster state (e.g., `GetWorker`, `ListWorkers`, `ListCollections`, `GetCollectionStatus`, `GetLeader`) directly access the local `s.fsm` via its thread-safe helper methods. This allows for high-performance, low-latency reads that are still linearizable because the FSM itself is managed by Raft.

#### `GetWorker` - The Service Discovery Mechanism

This is a critical RPC used by the `apigateway` and other clients to discover which worker node is responsible for a given data operation.

1.  **Collection Lookup:** It first retrieves the `Collection` information from the FSM.
2.  **Consistent Hashing:**
    *   If a `req.VectorId` is provided, it calculates a 64-bit FNV hash of the `vector_id`.
    *   It then iterates through the `collection.Shards` to find which shard's `KeyRangeStart` and `KeyRangeEnd` the hash falls into. This is the implementation of consistent hashing, ensuring that the same `vector_id` always maps to the same shard.
    *   If `req.VectorId` is empty (e.g., for collection-wide queries like search), it picks an arbitrary shard (currently the first one) for simplicity.
3.  **Worker Address Resolution:** Once the `targetShard` is identified, it retrieves the `WorkerInfo` for one of the replicas of that shard (currently the first replica in the list; a more sophisticated implementation might return the shard leader).
4.  It returns the `grpc_address` of the chosen worker and the `shard_id`.

#### `GetCollectionStatus`
Retrieves detailed metadata about a collection and its shards by querying the FSM. It also reports the `Ready` status of shards based on whether a leader has been reported.

### 7.4. `Heartbeat` (Read/Response Component)

The `Heartbeat` RPC also serves as the mechanism for the `placementdriver` to send shard assignments to workers.
1.  After processing the incoming heartbeat (and proposing relevant updates to the FSM), it reads the current FSM state to determine all `ShardAssignment`s for the heartbeating worker.
2.  It constructs a JSON-encoded list of these `ShardAssignment`s (including `ShardInfo` and `InitialMembers`) and sends it back to the worker in the `HeartbeatResponse.Message` field. This is how the `placementdriver` instructs workers on which shards they should host and what the initial Raft configuration for those shards should be.
