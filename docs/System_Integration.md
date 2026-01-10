# Vectron System Integration and Overview

This document provides a comprehensive overview of how the individual Vectron microservices integrate and interact to form a cohesive distributed vector database system. It synthesizes the detailed documentation of each component into a holistic view of the system's architecture, data flows, and operational characteristics.

## 1. High-Level System Architecture

The Vectron system is composed of several specialized microservices designed for scalability, fault tolerance, and efficient vector data management.

```mermaid
graph TD
    Client[External Client] -- 1. REST/gRPC API --> APIGateway
    APIGateway -- 2. gRPC Call (Auth) --> AuthService
    APIGateway -- 3. gRPC Call (PD - Metadata/Discovery) --> PlacementDriver
    APIGateway -- 4. gRPC Call (Worker - Data) --> WorkerService
    PlacementDriver -- 5. gRPC Heartbeat/Assignment --> WorkerService
    WorkerService -- 6. Raft Communication --> WorkerService
    WorkerService -- 7. PebbleDB/HNSW --> Local Storage
```

### Key Components:
*   **API Gateway:** The public-facing entry point, handling authentication, rate limiting, and request routing.
*   **Auth Service:** Manages user authentication and API key validation.
*   **Placement Driver:** The central coordinator, managing cluster metadata, workers, collections, and shard assignments. It forms its own Raft cluster for fault tolerance.
*   **Worker Service:** The stateful data nodes, responsible for storing vector data, building HNSW indexes, and performing vector searches. Workers manage replicated data shards using multi-Raft.
*   **Client Libraries:** Generated SDKs for various languages to interact with the API Gateway.

## 2. End-to-End Request Flow Examples

### 2.1. Client Registration & API Key Creation

1.  **Client -> API Gateway:** A new user registers via `RegisterUser` or logs in via `Login` to the API Gateway's REST/gRPC endpoint.
2.  **API Gateway -> Auth Service:** The API Gateway forwards these requests to the `AuthService` via gRPC.
3.  **Auth Service (internal):** The `AuthService` handles user creation/authentication, storing hashed passwords in `etcd`, and issuing a JWT token.
4.  **Client -> API Gateway (authenticated):** The user then uses the JWT to call `CreateAPIKey` on the API Gateway.
5.  **API Gateway -> Auth Service:** The API Gateway validates the JWT with the `AuthService` and then forwards the `CreateAPIKey` request.
6.  **Auth Service (internal):** The `AuthService` generates, hashes, and stores the API key in `etcd`, returning the plain-text key *once* to the client.

### 2.2. Collection Creation

1.  **Client -> API Gateway:** An authenticated client calls `CreateCollection` on the API Gateway, providing collection name, dimension, and distance metric. The API Gateway's Auth Middleware validates the API key (by calling `AuthService`).
2.  **API Gateway -> Placement Driver:** The API Gateway forwards the `CreateCollection` request to the `PlacementDriver` via gRPC.
3.  **Placement Driver (internal - Raft):** The `PlacementDriver` proposes a `CreateCollection` command to its internal Raft cluster.
4.  **Placement Driver (internal - FSM):** Upon Raft commitment, the `PlacementDriver`'s FSM deterministically creates initial shards for the collection, assigns replicas to available workers (e.g., round-robin), and stores this metadata.
5.  **Placement Driver -> API Gateway -> Client:** The success/failure is propagated back.

### 2.3. Vector Upsert (Data Ingestion)

1.  **Client -> API Gateway:** An authenticated client calls `Upsert` on the API Gateway with a collection name and `Point` data (ID, vector, payload).
2.  **API Gateway -> Placement Driver (Service Discovery):** For each `Point`, the API Gateway calls `GetWorker` on the `PlacementDriver`, providing the collection and `Point.ID`. The `PlacementDriver` uses consistent hashing to map the `Point.ID` to a specific shard and returns the gRPC address of a worker responsible for that shard.
3.  **API Gateway -> Worker Service:** The API Gateway opens a gRPC connection to the identified worker and calls `StoreVector`, translating the `apigatewaypb.Point` to `workerpb.Vector` using its `translator`.
4.  **Worker Service (internal - Raft):** The `WorkerService` proposes a `StoreVector` command to the Raft cluster of the specific shard.
5.  **Worker Service (internal - FSM/Storage):** Upon Raft commitment, the worker's State Machine applies the `StoreVector` command:
    *   Adds the vector to the in-memory HNSW index.
    *   Persists the vector and its metadata to PebbleDB.
    *   Writes an entry to the HNSW WAL for durability.
6.  **Worker Service -> API Gateway -> Client:** Success/failure propagated back.

### 2.4. Vector Search (Querying)

1.  **Client -> API Gateway:** An authenticated client calls `Search` on the API Gateway with a collection name, query vector, and `top_k`.
2.  **API Gateway -> Placement Driver (Service Discovery):** The API Gateway calls `GetWorker` on the `PlacementDriver`, providing only the collection name (as search can happen on any replica). The `PlacementDriver` returns a worker address for a replica of a shard in that collection.
3.  **API Gateway -> Worker Service:** The API Gateway opens a gRPC connection to the identified worker and calls `Search`, translating the request.
4.  **Worker Service (internal - Read Path):** The `WorkerService` performs a linearizable read (`SyncRead`) on the shard's FSM. The FSM's `Lookup` method uses the in-memory HNSW index to find approximate nearest neighbors.
5.  **Worker Service -> API Gateway -> Client:** The search results (IDs and scores) are returned, translated back, and sent to the client.

## 3. Consistency Model

Vectron primarily targets **linearizable consistency** for both read and write operations on its critical metadata and data paths.

*   **Placement Driver:**
    *   **Writes (e.g., `CreateCollection`, `RegisterWorker`):** Achieved through Raft's `SyncPropose`. A command is only considered successful after being replicated to a quorum and applied to the FSM on the leader.
    *   **Reads (e.g., `GetWorker`, `ListCollections`):** Achieved by serving directly from the local Raft FSM, which itself ensures linearizability.
*   **Worker Service:**
    *   **Writes (e.g., `StoreVector`, `DeleteVector`):** Achieved through Raft's `SyncPropose`. Operations are committed to the shard's Raft log and applied to the FSM on the shard leader, then replicated.
    *   **Reads (e.g., `Search`, `GetVector`):** Achieved through Raft's `SyncRead`, ensuring that the read reflects the latest committed state.

## 4. Fault Tolerance and Resilience

The system is designed with several layers of fault tolerance:

*   **Placement Driver Cluster:** The `placementdriver` itself runs as a Raft cluster. If a `placementdriver` node fails, Raft automatically elects a new leader, ensuring continuous availability of cluster coordination.
*   **Worker Service Multi-Raft:** Worker nodes host multiple Raft groups (shards). If a worker node fails, the affected shards' Raft groups can elect new leaders on surviving replicas, minimizing data unavailability.
*   **API Gateway Leader Discovery:** The API Gateway (and worker clients) implement logic to discover the current `placementDriver` leader. This allows them to seamlessly switch to a new leader if the primary one fails.
*   **Worker Heartbeats:** Workers send periodic heartbeats to the `placementdriver`. This allows the `placementdriver` to detect failed workers and initiate rebalancing or replica promotion as needed.
*   **HNSW Index Persistence (Workers):** The worker's HNSW index, though in-memory, is made durable through a two-pronged approach of periodic full snapshots and a write-ahead log (WAL) to PebbleDB. This allows for fast recovery on worker restart.
*   **Raft Snapshotting (PD & Workers):** Both the `placementdriver` and `worker` Raft FSMs implement snapshotting. This allows new or lagging replicas to quickly catch up to the current state without replaying the entire Raft log from the beginning.

## 5. Architectural Diagrams (Conceptual)

### 5.1. Overall System Data Flow

```
+------------+       +-------------+       +-------------------+       +-------------------+
|   Client   | <---> | API Gateway | <---> |  Placement Driver | <---> |    Worker Node(s)   |
| (Web/SDK)  |       | (stateless) |       | (Raft Cluster)    |       | (Multi-Raft, HNSW)  |
+------------+       +-------------+       +-------------------+       +-------------------+
        ^                    |                       ^                           |
        |                    |                       |                           |
        +--------------------+                       +---------------------------+
             Auth Service (etcd)                        (PebbleDB per shard)
```

### 5.2. Shard Management in Worker Service

```
+-----------------------------------------------------------------+
|                         Worker Node                             |
| +-------------------------------------------------------------+ |
| |                        Dragonboat NodeHost                    |
| | +---------------------------------------------------------+ | |
| | |                  Shard Manager (Reconciliation)         | | |
| | |                                                         | | |
| | +---------------------------------------------------------+ | |
| |                             |                               | |
| |                             v                               | |
| | +---------------------------------------------------------+ | |
| | |                       Raft Group (Shard 1)              | | |
| | | +-----------------------------------------------------+ | | |
| | | |                   State Machine                     | | | |
| | | | +-------------------------------------------------+ | | | |
| | | | |  PebbleDB (Vector Data + WAL) + In-Memory HNSW  | | | | |
| | | | +-------------------------------------------------+ | | | |
| | | +-----------------------------------------------------+ | | |
| | +---------------------------------------------------------+ | |
| |                                 ...                           | |
| | +---------------------------------------------------------+ | |
| | |                       Raft Group (Shard N)              | | |
| | | +-----------------------------------------------------+ | | |
| | | |                   State Machine                     | | | |
| | | | +-------------------------------------------------+ | | | |
| | | | |  PebbleDB (Vector Data + WAL) + In-Memory HNSW  | | | | |
| | | | +-------------------------------------------------+ | | | |
| | | +-----------------------------------------------------+ | | |
| | +---------------------------------------------------------+ | |
| +-------------------------------------------------------------+ |
+-----------------------------------------------------------------+
```
