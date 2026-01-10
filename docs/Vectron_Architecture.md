# Vectron System Architecture Overview

This document outlines the high-level architecture of the Vectron distributed vector database system, detailing its core components and their interactions.

## 1. High-Level System Architecture

The Vectron system is composed of several specialized microservices designed for scalability, fault tolerance, and efficient vector data management.

```mermaid
graph TD
    Client[External Client] -- 1. REST/gRPC API (User Creds/API Key) --> APIGateway
    APIGateway -- 2. gRPC Call (Validate API Key) --> AuthService
    AuthService -- API Key Validity (UserID, Plan) --> APIGateway
    APIGateway -- 3. gRPC Call (Collection Metadata/Shard Discovery) --> PlacementDriver
    PlacementDriver -- Worker/Shard Address --> APIGateway
    APIGateway -- 4. gRPC Call (Vector Data/Search Query) --> WorkerService
    WorkerService -- 5. gRPC Heartbeat (Shard Leader Info) --> PlacementDriver
    PlacementDriver -- Shard Assignments (JSON) --> WorkerService
    WorkerService -- 6. Raft Communication (Proposals, Logs, Snapshots) --> WorkerService
    WorkerService -- 7. PebbleDB/HNSW (Vector Data, Index State) --> Local Storage
```

### Key Components:

- **API Gateway:** The public-facing entry point, handling authentication, rate limiting, and request routing.
- **Auth Service:** Manages user authentication and API key validation.
- **Placement Driver:** The central coordinator, managing cluster metadata, workers, collections, and shard assignments. It forms its own Raft cluster for fault tolerance.
- **Worker Service:** The stateful data nodes, responsible for storing vector data, building HNSW indexes, and performing vector searches. Workers manage replicated data shards using multi-Raft.
- **Client Libraries:** Generated SDKs for various languages to interact with the API Gateway.

## 2. End-to-End Request Flow Examples

### 2.1. Client Registration & API Key Creation

1.  **Client -> API Gateway:** A new user registers via `RegisterUser` or logs in via `Login` to the API Gateway's REST/gRPC endpoint, sending user credentials (email, password).
2.  **API Gateway -> Auth Service:** The API Gateway forwards these requests (user credentials) to the `AuthService` via gRPC.
3.  **Auth Service (internal):** The `AuthService` handles user creation/authentication, storing hashed passwords in `etcd`, and issuing a JWT token (session token for frontend).
4.  **Client -> API Gateway (authenticated):** The user then uses the obtained JWT to call `CreateAPIKey` on the API Gateway, providing a key name.
5.  **API Gateway -> Auth Service:** The API Gateway's Auth Middleware validates the JWT with the `AuthService` (via `ValidateAPIKey` RPC, sending the JWT). The `AuthService` returns `UserID` and `Plan`. The `CreateAPIKey` request is then forwarded to the `AuthService`.
6.  **Auth Service (internal):** The `AuthService` generates a new API key, hashes it, stores the hashed key in `etcd`, and returns the plain-text key _once_ to the client.

### 2.2. Collection Creation

1.  **Client -> API Gateway:** An authenticated client calls `CreateCollection` on the API Gateway, providing collection name, dimension, and distance metric. The API Gateway's Auth Middleware validates the API key (by calling `AuthService` with the API key from the request header, receiving `UserID` and `Plan`).
2.  **API Gateway -> Placement Driver:** The API Gateway forwards the `CreateCollection` request (collection name, dimension, distance) to the `PlacementDriver` via gRPC.
3.  **Placement Driver (internal - Raft):** The `PlacementDriver` proposes a `CreateCollection` command (containing collection metadata) to its internal Raft cluster.
4.  **Placement Driver (internal - FSM):** Upon Raft commitment, the `PlacementDriver`'s FSM deterministically creates initial shards for the collection, assigns replicas to available workers (e.g., round-robin), and stores this metadata in its in-memory state.
5.  **Placement Driver -> API Gateway -> Client:** The success/failure status of the operation is propagated back.

### 2.3. Vector Upsert (Data Ingestion)

1.  **Client -> API Gateway:** An authenticated client calls `Upsert` on the API Gateway with a collection name and `Point` data (ID, vector, payload).
2.  **API Gateway -> Placement Driver (Service Discovery):** For each `Point` to be upserted, the API Gateway calls `GetWorker` on the `PlacementDriver`, providing the collection name and `Point.ID`. The `PlacementDriver` uses consistent hashing to map the `Point.ID` to a specific shard and returns the gRPC address of a worker responsible for that shard, along with the `shard_id`.
3.  **API Gateway -> Worker Service:** The API Gateway opens a gRPC connection to the identified worker and calls `StoreVector`, translating the `apigatewaypb.Point` (ID, vector, payload) to `workerpb.Vector` (ID, vector, serialized metadata) using its `translator`.
4.  **Worker Service (internal - Raft):** The `WorkerService` proposes a `StoreVector` command (containing `shard_id` and serialized `workerpb.Vector`) to the Raft cluster of the specific shard.
5.  **Worker Service (internal - FSM/Storage)::** Upon Raft commitment, the worker's State Machine applies the `StoreVector` command:
    - Adds the vector to the in-memory HNSW index.
    - Persists the vector and its metadata to PebbleDB.
    - Writes an entry to the HNSW WAL for durability.
6.  **Worker Service -> API Gateway -> Client:** Success/failure propagated back to the client.

### 2.4. Vector Search (Querying)

1.  **Client -> API Gateway:** An authenticated client calls `Search` on the API Gateway with a collection name, query vector, and `top_k`.
2.  **API Gateway -> Placement Driver (Service Discovery):** The API Gateway calls `GetWorker` on the `PlacementDriver`, providing only the collection name (as search can happen on any replica, `vector_id` is empty). The `PlacementDriver` returns a worker address for a replica of a shard in that collection, along with the `shard_id`.
3.  **API Gateway -> Worker Service:** The API Gateway opens a gRPC connection to the identified worker and calls `Search`, translating the `apigatewaypb.SearchRequest` (query vector, `top_k`) to `workerpb.SearchRequest` (shard ID, query vector, `k`).
4.  **Worker Service (internal - Read Path):** The `WorkerService` performs a linearizable read (`SyncRead`) on the shard's FSM with the search query. The FSM's `Lookup` method uses the in-memory HNSW index to find approximate nearest neighbors (returning IDs and scores).
5.  **Worker Service -> API Gateway -> Client:** The search results (IDs and scores) are returned from the worker, translated back into `apigatewaypb.SearchResult` and sent to the client.

## 3. Core Architectural Principles and Technologies

### 3.1. Microservices Architecture

Vectron is built as a set of independent, loosely coupled services:

- **Auth Service:** Manages user authentication and API keys.
- **API Gateway:** Public interface, handles authentication, routing, and cross-cutting concerns.
- **Placement Driver:** Central coordinator for cluster state, sharding, and worker management.
- **Worker Service:** Stateful data nodes storing and querying vectors.

### 3.2. gRPC for Inter-Service Communication

All internal and public APIs (via gRPC-Gateway) utilize gRPC and Protocol Buffers for efficient, high-performance, and type-safe communication.

### 3.3. Raft Consensus Protocol

Raft is fundamental to Vectron's fault tolerance and consistency:

- **Placement Driver:** The `placementdriver` itself is a Raft cluster, ensuring its metadata (worker list, collection definitions, shard assignments) is strongly consistent and durable.
- **Worker Service:** Workers employ a "multi-Raft" architecture, where each data shard is an independent Raft consensus group. This provides replicated storage and linearizable consistency for vector data.

### 3.4. Consistent Hashing

The `Placement Driver` uses consistent hashing based on vector IDs to deterministically map vectors to specific shards, enabling efficient data retrieval and routing by the API Gateway.

### 3.5. HNSW Indexing for Vector Search

The `Worker` service uses the Hierarchical Navigable Small World (HNSW) algorithm for Approximate Nearest Neighbor (ANN) search. This in-memory index provides sub-millisecond query times for vector similarity searches, with a robust persistence mechanism (snapshots + WAL) for durability.

### 3.6. PebbleDB for Persistent Storage

Workers utilize PebbleDB, a RocksDB-inspired key-value store, for persistent storage of vector data and HNSW index snapshots/WAL entries. This provides a durable and performant foundation for the worker's state.

## 4. Fault Tolerance and Resilience

Vectron is designed with multiple layers of fault tolerance:

- **Raft-backed Services:** Both the `Placement Driver` and `Worker` services rely on Raft for strong consistency and automatic leader election/failover, ensuring continuous operation despite node failures.
- **Leader Discovery:** The `API Gateway` and `Worker` clients include mechanisms to discover the current leader of the `Placement Driver` cluster, seamlessly adapting to leader changes.
- **Worker Heartbeats:** Workers periodically report their status to the `Placement Driver`, allowing it to detect and react to worker failures.
- **Shard Replication:** Data shards are replicated across multiple workers, so the loss of a worker does not lead to data loss and services can continue via other replicas.

## 5. Simplified Component Interaction

To summarize the core interactions:

graph TD
A[Client] -- REST/gRPC API (Requests) --> B(API Gateway)
B -- Validate API Key (Credentials) --> C(Auth Service)
B -- Get Shard/Worker (Collection, Vector ID) --> D(Placement Driver)
B -- Vector Data/Search Query --> E(Worker Service)
D -- Worker Heartbeats/Assignments --> E
E -- Raft Consensus (Replication) --> E
D -- Raft Consensus (Coordination) --> D

    ---

```
+-----------------------------------------------------------------------------------------+
|                                  Vectron System Overview                                |
+-----------------------------------------------------------------------------------------+
|                                                                                         |
| +----------+      (REST/gRPC API)        +--------------+      (Validate API Key)     |
| |  Client  | ---------------------------> | API Gateway  | ---------------------------> |
| |          | <--------------------------- | (stateless)  | <--------------------------- |
| +----------+      (Responses)            +--------------+      (Auth Status)          |
|                                                |                                        |
|                                                | (Get Shard/Worker Info)                |
|                                                V                                        |
|                                          +----------------+                             |
|                                          | Placement      |                             |
|                                          | Driver         |                             |
|                                          | (Raft Cluster) |                             |
|                                          +----------------+                             |
|                                                ^       |                                |
|                                                |       | (Worker Heartbeats)            |
|                                                |       |                                |
| (Vector Data/Search Query)                     |       V                                |
| ---------------------------------------------> |   +-----------------------+            |
| <--------------------------------------------- |   |    Worker Service     |            |
| (Results)                                      |   | (Multi-Raft, HNSW,    |            |
|                                                |   |  PebbleDB per Shard)  |            |
|                                                |   +-----------------------+            |
|                                                |                                        |
|                                                <----------------------------------------+
|                                                 (Shard Assignments)
|
+-----------------------------------------------------------------------------------------+
```

---

                         ┌──────────────────────┐
                         │     External Client   │
                         │   (SDK / Application)  │
                         └───────────┬──────────┘
                                     │ REST / gRPC
                                     ▼

┌─────────────────────┐ ┌───────────────────────┐ ┌────────────────────┐
│ │ │ │ │ │
│ Auth Service ◄──────┤ API Gateway ├──────► Placement Driver │
│ (Users, API Keys, │ JWT/ │ (Auth middleware, │ gRPC │ (Cluster metadata, │
│ JWT validation, │ Key │ rate limiting, │ │ shard placement, │
│ etcd storage) │ │ request routing) │ │ consistent hash, │
│ │ │ │ │ Raft cluster) │
└───────────┬─────────┘ └───────────┬───────────┘ └───────────┬────────┘
│ │ │
│ │ gRPC │ Shard assignments
│ ▼ │ & Worker addresses
│ ┌───────────────────────┐ │
│ │ │ │
└──────────────────►│ Worker Service │◄─────────────────┘
│ (multiple instances) │
│ - Stores vectors │
│ - Builds HNSW index │
│ - PebbleDB storage │
│ - Multi-Raft per shard│
└───────────┬───────────┘
│
▼
┌───────────────────┐
│ Local Storage │
│ • PebbleDB │
│ • HNSW index files│
│ • WAL │
└───────────────────┘

Intra-shard replication (Raft):
Worker ───► Worker ───► Worker (same shard, different nodes)
▲ ▲ ▲
└──────── Raft consensus group ──────┘

---

Client
│
▼ Heartbeats / Registration
API Gateway ◄──────────────────────────────────────────┐
│ ▲ │
│ │ API Key validation │
▼ │ │
Auth Service │
│
│ ▲ │
│ │ Collection info / GetWorker (shard location) │
▼ │ │
Placement Driver ──────────────────────────────────────┘
│ ▲
│ │ gRPC StoreVector / Search / ...
▼ │
Worker Service ───────┐
▲ ▼ │ Raft per shard
│ └───────────────┴──────────────┐
│ Replication ▼
└─────────────────────────────► Other Workers (replicas)

      ---
