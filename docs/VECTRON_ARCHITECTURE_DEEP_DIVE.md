# Vectron Architecture Deep Dive
## Complete System Flow and Behavioral Analysis

---

## Table of Contents

1. [System Overview](#1-system-overview)
2. [Architecture Layers](#2-architecture-layers)
3. [Data Flow Deep Dive](#3-data-flow-deep-dive)
4. [Control Flow Analysis](#4-control-flow-analysis)
5. [Component Interactions](#5-component-interactions)
6. [State Machines](#6-state-machines)
7. [Failure Scenarios](#7-failure-scenarios)
8. [Performance Characteristics](#8-performance-characteristics)
9. [Deployment Architecture](#9-deployment-architecture)

---

## 1. System Overview

### 1.1 High-Level Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CLIENT LAYER                                    │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐                    │
│  │ Web Apps │  │ Mobile   │  │ CLI Tool │  │ Python   │                    │
│  │          │  │ Apps     │  │          │  │ Scripts  │                    │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘                    │
└───────┼─────────────┼─────────────┼─────────────┼──────────────────────────┘
        │             │             │             │
        ▼             ▼             ▼             ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           API GATEWAY LAYER                                  │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │  HTTP/gRPC Server  │  Auth Middleware  │  Rate Limiter  │  Logger    │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                    │                                        │
│  ┌─────────────────────────────────┼─────────────────────────────────────┐  │
│  │                                 ▼                                     │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                │  │
│  │  │   Router     │──│    Cache     │──│  Aggregator  │                │  │
│  │  │  (Consistent │  │  (TinyLFU)   │  │ (Min-Heap)   │                │  │
│  │  │   Hashing)   │  └──────────────┘  └──────────────┘                │  │
│  │  └──────────────┘                                                   │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────┬───────────────────────────────────────────┘
                                  │
          ┌───────────────────────┼───────────────────────┐
          │                       │                       │
          ▼                       ▼                       ▼
┌─────────────────┐  ┌────────────────────┐  ┌────────────────────┐
│  AUTH SERVICE   │  │  PLACEMENT DRIVER  │  │     RERANKER       │
│  ┌───────────┐  │  │  ┌──────────────┐  │  │  ┌──────────────┐  │
│  │  User DB  │  │  │  │   Raft FSM   │  │  │  │  Rule Engine │  │
│  │  (etcd)   │  │  │  │   (3-5 nodes)│  │  │  │  TF-IDF      │  │
│  └───────────┘  │  │  └──────────────┘  │  │  └──────────────┘  │
│                 │  │                    │  │                    │
│  ┌───────────┐  │  │  ┌──────────────┐  │  │  ┌──────────────┐  │
│  │   JWT     │  │  │  │   Worker     │  │  │  │    Cache     │  │
│  │  Issuer   │  │  │  │   Registry   │  │  │  │   (Redis)    │  │
│  └───────────┘  │  │  └──────────────┘  │  │  └──────────────┘  │
└─────────────────┘  └────────────────────┘  └────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           WORKER NODE LAYER                                  │
│                                                                              │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐          │
│  │    WORKER 1      │  │    WORKER 2      │  │    WORKER N      │          │
│  │  ┌────────────┐  │  │  ┌────────────┐  │  │  ┌────────────┐  │          │
│  │  │ NodeHost   │  │  │  │ NodeHost   │  │  │  │ NodeHost   │  │          │
│  │  │(Dragonboat)│  │  │  │(Dragonboat)│  │  │  │(Dragonboat)│  │          │
│  │  └────────────┘  │  │  └────────────┘  │  │  └────────────┘  │          │
│  │        │         │  │        │         │  │        │         │          │
│  │  ┌─────┴─────┐   │  │  ┌─────┴─────┐   │  │  ┌─────┴─────┐   │          │
│  │  │  Shard 1  │   │  │  │  Shard 3  │   │  │  │  Shard 5  │   │          │
│  │  │ ┌───────┐ │   │  │  │ ┌───────┐ │   │  │  │ ┌───────┐ │   │          │
│  │  │ │Pebble │ │   │  │  │ │Pebble │ │   │  │  │ │Pebble │ │   │          │
│  │  │ │  DB   │ │   │  │  │ │  DB   │ │   │  │  │ │  DB   │ │   │          │
│  │  │ └───────┘ │   │  │  │ └───────┘ │   │  │  │ └───────┘ │   │          │
│  │  │ ┌───────┐ │   │  │  │ ┌───────┐ │   │  │  │ ┌───────┐ │   │          │
│  │  │ │ HNSW  │ │   │  │  │ │ HNSW  │ │   │  │  │ │ HNSW  │ │   │          │
│  │  │ │ Index │ │   │  │  │ │ Index │ │   │  │  │ │ Index │ │   │          │
│  │  │ └───────┘ │   │  │  │ └───────┘ │   │  │  │ └───────┘ │   │          │
│  │  └───────────┘   │  │  └───────────┘   │  │  └───────────┘   │          │
│  │  ┌───────────┐   │  │  ┌───────────┐   │  │  ┌───────────┐   │          │
│  │  │  Shard 2  │   │  │  │  Shard 4  │   │  │  │  Shard 6  │   │          │
│  │  └───────────┘   │  │  └───────────┘   │  │  └───────────┘   │          │
│  └──────────────────┘  └──────────────────┘  └──────────────────┘          │
│                                                                              │
│  [Inter-Worker Communication: gRPC for WAL streaming during rebalancing]    │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1.2 Component Summary

**API Gateway (The Front Door)**
- **Role**: Single entry point for all client requests
- **Responsibilities**: Authentication, routing, caching, aggregation
- **Technology**: Go, gRPC, grpc-gateway, Redis
- **Key Features**: TinyLFU cache, circuit breakers, request coalescing

**Placement Driver (The Brain)**
- **Role**: Control plane managing cluster topology
- **Responsibilities**: Worker registry, shard assignment, load balancing
- **Technology**: Go, Dragonboat (Raft), custom FSM
- **Key Features**: Failure domain awareness, automatic rebalancing

**Worker Nodes (The Muscle)**
- **Role**: Data plane storing vectors and executing queries
- **Responsibilities**: Vector storage, HNSW indexing, Raft consensus
- **Technology**: Go, PebbleDB, Dragonboat, AVX2 SIMD
- **Key Features**: Quantization, hot/cold tiering, WAL streaming

**Auth Service (The Gatekeeper)**
- **Role**: User authentication and authorization
- **Responsibilities**: User management, JWT issuance, API key validation
- **Technology**: Go, bcrypt, etcd
- **Key Features**: Secure credential storage, token refresh

**Reranker (The Refiner)** [Optional]
- **Role**: Post-processing search results
- **Responsibilities**: Relevance boosting, metadata matching
- **Technology**: Go, TF-IDF, Redis cache
- **Key Features**: Pluggable strategies, result caching

---

## 2. Architecture Layers

### 2.1 Layer 1: Client Interface Layer

```
┌─────────────────────────────────────────────────────────────┐
│                    CLIENT INTERFACE                          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    │
│  │  REST API   │    │  gRPC API   │    │  WebSocket  │    │
│  │             │    │             │    │  (Stream)   │    │
│  │  /v1/collections │  CreateCollection  │  SearchStream   │    │
│  │  /v1/search      │  Search            │                 │    │
│  │  /v1/upsert      │  Upsert            │                 │    │
│  └─────────────┘    └─────────────┘    └─────────────┘    │
│                                                             │
│  Protocol Translation:                                      │
│  HTTP/JSON ↔ gRPC/Protobuf                                  │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Behavior**: Clients can connect via REST (for simplicity) or gRPC (for performance). The gateway handles protocol translation transparently.

### 2.2 Layer 2: API Gateway Services

```
┌─────────────────────────────────────────────────────────────┐
│                    API GATEWAY SERVICES                      │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              HTTP/gRPC Server                        │   │
│  │   - TLS termination                                  │   │
│  │   - Request parsing                                  │   │
│  │   - Protocol validation                              │   │
│  └────────────────┬────────────────────────────────────┘   │
│                   │                                         │
│                   ▼                                         │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              Middleware Chain                        │   │
│  │   1. Rate Limiter (Token bucket)                     │   │
│  │   2. Auth Middleware (JWT validation)                │   │
│  │   3. Logging (Structured logs)                       │   │
│  │   4. Metrics (Prometheus)                            │   │
│  └────────────────┬────────────────────────────────────┘   │
│                   │                                         │
│                   ▼                                         │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              Request Router                          │   │
│  │   - Collection → Shards mapping                      │   │
│  │   - Consistent hashing (FNV-1a)                      │   │
│  │   - Worker selection                                 │   │
│  └────────────────┬────────────────────────────────────┘   │
│                   │                                         │
│         ┌─────────┴──────────┐                              │
│         ▼                    ▼                              │
│  ┌──────────────┐     ┌──────────────┐                     │
│  │    Cache     │     │   Backend    │                     │
│  │   (TinyLFU)  │     │    Calls     │                     │
│  │              │     │              │                     │
│  │  Check:      │     │  gRPC to     │                     │
│  │  Collection  │     │  Workers     │                     │
│  │  + Vector    │     │              │                     │
│  │  + TopK      │     │              │                     │
│  └──────────────┘     └──────────────┘                     │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Key Behaviors**:
- Rate limiting uses token bucket algorithm with per-API-key quotas
- JWT validation checks signature, expiration, and claims
- Router caches shard mappings with 2-second TTL
- Cache uses 128 shards with independent TinyLFU instances

### 2.3 Layer 3: Control Plane (Placement Driver)

```
┌─────────────────────────────────────────────────────────────┐
│                    CONTROL PLANE                             │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │         Raft Consensus Cluster (3-5 nodes)           │   │
│  │                                                     │   │
│  │    Node 1 (Leader)     Node 2        Node 3        │   │
│  │    ┌──────────┐       ┌──────────┐   ┌──────────┐  │   │
│  │    │   Log    │◄─────►│   Log    │◄──►│   Log    │  │   │
│  │    │   [0]    │       │   [0]    │   │   [0]    │  │   │
│  │    │   [1]    │       │   [1]    │   │   [1]    │  │   │
│  │    │   [2]◄───┼───────┼──►[2]    │   │   [2]◄───┼──┤   │
│  │    └──────────┘       └──────────┘   └──────────┘  │   │
│  │         ▲                                          │   │
│  │         │ AppendEntries RPC                        │   │
│  └─────────┼──────────────────────────────────────────┘   │
│            │                                                │
│            ▼                                                │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              Finite State Machine                    │   │
│  │                                                     │   │
│  │  Workers: map[workerID]→WorkerInfo                  │   │
│  │    - Address, Capacity, Load, Health                │   │
│  │    - Failure Domain (Rack/Zone/Region)              │   │
│  │                                                     │   │
│  │  Collections: map[name]→Collection                  │   │
│  │    - Dimension, Distance Metric                     │   │
│  │    - Shards: []ShardInfo                            │   │
│  │                                                     │   │
│  │  Shards: map[shardID]→ShardInfo                     │   │
│  │    - Key Range (Start, End)                         │   │
│  │    - Replicas: []WorkerID                           │   │
│  │    - Leader: WorkerID                               │   │
│  │    - Epoch: uint64 (monotonic)                      │   │
│  │                                                     │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  Background Reconciler (every 30s):                        │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ 1. Check worker health (heartbeat timeout)          │   │
│  │ 2. Detect load imbalance (CPU/memory variance >20%) │   │
│  │ 3. Identify hot shards (QPS > threshold)            │   │
│  │ 4. Trigger rebalancing if needed                    │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Raft Behavior**:
- Leader election timeout: 1-2 seconds (randomized)
- Heartbeat interval: 100ms
- Log entries committed on majority (2 of 3, 3 of 5)
- Snapshot every 10,000 entries or 100MB

### 2.4 Layer 4: Data Plane (Worker Nodes)

```
┌─────────────────────────────────────────────────────────────┐
│                    DATA PLANE - WORKER NODE                  │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │         Dragonboat NodeHost                          │   │
│  │         (Manages multiple Raft groups)               │   │
│  │                                                     │   │
│  │  ┌───────────┐ ┌───────────┐ ┌───────────┐         │   │
│  │  │  Shard 1  │ │  Shard 2  │ │  Shard N  │         │   │
│  │  │  Raft     │ │  Raft     │ │  Raft     │         │   │
│  │  │  Group    │ │  Group    │ │  Group    │         │   │
│  │  │  (ID:101) │ │  (ID:102) │ │  (ID:10N) │         │   │
│  │  └─────┬─────┘ └─────┬─────┘ └─────┬─────┘         │   │
│  │        │             │             │               │   │
│  └────────┼─────────────┼─────────────┼───────────────┘   │
│           │             │             │                    │
│           ▼             ▼             ▼                    │
│  ┌─────────────────────────────────────────────────────┐   │
│  │         Per-Shard State Machine                      │   │
│  │                                                     │   │
│  │  ┌──────────────────────────────────────────────┐  │   │
│  │  │           PebbleDB Storage                    │  │   │
│  │  │  ┌────────────────────────────────────────┐  │  │   │
│  │  │  │ Key: v_<vectorID>                      │  │  │   │
│  │  │  │ Value: {vector, metadata, timestamp}   │  │  │   │
│  │  │  └────────────────────────────────────────┘  │  │   │
│  │  │  ┌────────────────────────────────────────┐  │  │   │
│  │  │  │ Key: hnsw_<layer>_<nodeID>             │  │  │   │
│  │  │  │ Value: {neighbors[], level}            │  │  │   │
│  │  │  └────────────────────────────────────────┘  │  │   │
│  │  └──────────────────────────────────────────────┘  │   │
│  │                      │                              │   │
│  │                      ▼                              │   │
│  │  ┌──────────────────────────────────────────────┐  │   │
│  │  │          HNSW Index (In-Memory)               │  │   │
│  │  │                                             │  │   │
│  │  │  Layer 3: [Entry Point]                    │  │   │
│  │  │       │                                     │  │   │
│  │  │       ▼                                     │  │   │
│  │  │  Layer 2: [Nodes] ◄──► [Nodes]            │  │   │
│  │  │       │              │                     │  │   │
│  │  │       ▼              ▼                     │  │   │
│  │  │  Layer 1: [Nodes] ◄──► [Nodes] ◄──► [..]  │  │   │
│  │  │       │              │                     │  │   │
│  │  │       ▼              ▼                     │  │   │
│  │  │  Layer 0: [All Vectors] ◄──► [Neighbors]  │  │   │
│  │  │                                             │  │   │
│  │  │  Quantized: int8[] (75% memory savings)    │  │   │
│  │  └──────────────────────────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │         WAL Hub (Replication)                        │   │
│  │  - Publishes updates to subscribers                  │   │
│  │  - Used for search-only node streaming               │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Worker Behavior**:
- Each worker can host 10-100+ shards depending on size
- Shards are independent Raft groups
- HNSW index is built asynchronously from PebbleDB
- WAL Hub enables search-only replicas

---

## 3. Data Flow Deep Dive

### 3.1 Vector Ingestion Flow (Write Path)

```
┌──────────────────────────────────────────────────────────────────────┐
│                    VECTOR INGESTION FLOW                             │
└──────────────────────────────────────────────────────────────────────┘

Step 1: Client Request
──────────────────────
Client ──► API Gateway

POST /v1/collections/my_collection/points
{
  "points": [
    {"id": "vec_001", "vector": [0.1, 0.2, ...], "payload": {"key": "value"}},
    {"id": "vec_002", "vector": [0.3, 0.4, ...], "payload": {"key": "value"}},
    ... (batch of 100-1000 vectors)
  ]
}

Step 2: Authentication & Validation
────────────────────────────────────
API Gateway:
  ├─► Validate JWT token
  ├─► Check rate limits (token bucket)
  ├─► Validate collection exists (cache check)
  └─► Validate vector dimensions match collection

Step 3: Shard Routing
─────────────────────
API Gateway ──► Consistent Hashing

For each vector ID:
  shard_id = FNV-1a(vector_id) % num_shards
  worker = placement_driver.GetWorker(shard_id)

Example:
  "vec_001" ──► Hash: 0x8A3F... ──► Shard 5 ──► Worker 2
  "vec_002" ──► Hash: 0x2B91... ──► Shard 12 ──► Worker 3
  "vec_003" ──► Hash: 0x5E72... ──► Shard 5 ──► Worker 2

Grouping by shard:
  Worker 2: [vec_001, vec_003]
  Worker 3: [vec_002]

Step 4: Batch RPC to Workers
────────────────────────────
API Gateway ──► gRPC BatchStoreVector

Parallel RPCs to all affected workers:
  ├─► Worker 2: BatchStoreVector(shard=5, vectors=[vec_001, vec_003])
  └─► Worker 3: BatchStoreVector(shard=12, vectors=[vec_002])

Step 5: Raft Consensus (Per Shard)
───────────────────────────────────
Worker 2 (Shard 5):
  ├─► Propose StoreVectorBatch to Raft group
  ├─► Leader appends to log
  ├─► Replicate to followers (Worker 1, Worker 4)
  ├─► Wait for majority acknowledgment
  └─► Commit entry

Timeline:
  T+0ms: Proposal received
  T+1ms: Leader appends
  T+2ms: Replicate to followers
  T+3ms: Followers acknowledge
  T+4ms: Entry committed

Step 6: State Machine Application
──────────────────────────────────
Worker 2 (Shard 5):
  ├─► Decode command from committed entry
  ├─► Write vectors to PebbleDB:
  │     Key: "v_vec_001", Value: {vector, metadata}
  │     Key: "v_vec_003", Value: {vector, metadata}
  ├─► Add to HNSW indexing queue (async)
  └─► Update applied index

Step 7: Async HNSW Indexing
───────────────────────────
Background Goroutine:
  ├─► Dequeue vectors from indexing queue
  ├─► Quantize vectors (float32 → int8)
  ├─► Insert into HNSW graph:
  │     1. Select random level (exponential distribution)
  │     2. Find entry point at top level
  │     3. Navigate down to insertion level
  │     4. Connect to M nearest neighbors
  ├─► Update in-memory HNSW structure
  └─► Optional: Persist graph to PebbleDB

Step 8: Response
────────────────
Worker 2 ──► API Gateway ──► Client

Response: {"upserted": 3}
(Note: Response sent after Raft commit, not HNSW indexing)
```

**Timing Breakdown**:
- Gateway processing: 1-2ms
- Shard routing: 0.5ms
- Raft consensus: 3-5ms (cross-AZ)
- PebbleDB write: 1ms
- **Total acknowledgment**: 5-8ms
- HNSW indexing (async): 10-50ms per batch

### 3.2 Search Query Flow (Read Path)

```
┌──────────────────────────────────────────────────────────────────────┐
│                    SEARCH QUERY FLOW                                 │
└──────────────────────────────────────────────────────────────────────┘

Step 1: Client Request
──────────────────────
Client ──► API Gateway

POST /v1/collections/my_collection/points/search
{
  "vector": [0.12, 0.34, 0.56, ...],  // 768 dimensions
  "top_k": 10,
  "query": "optional text for reranking"
}

Step 2: Cache Check
───────────────────
API Gateway:
  ├─► Compute cache key: 
  │   key = hash(collection + normalize(vector) + top_k)
  ├─► Check TinyLFU cache (128 shards)
  │
  ├─► CACHE HIT (68% of queries):
  │   └─► Return cached results immediately (0.5ms)
  │
  └─► CACHE MISS (32% of queries):
      └─► Continue to backend query

Step 3: Request Coalescing
──────────────────────────
API Gateway:
  ├─► Check for identical concurrent queries
  ├─► If found, add to waiting list
  └─► Only 1 query goes to backend

  Waiting Clients ──► Single Backend Query ──► All Clients

Step 4: Shard Resolution
────────────────────────
API Gateway ──► Placement Driver

Get all shards for collection "my_collection":
  Shards: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16]

Step 5: Broadcast Search
────────────────────────
API Gateway ──► Parallel gRPC to all shard leaders

For each shard:
  ├─► Get leader worker from cached topology
  ├─► Send Search RPC:
  │   {
  │     "shard_id": 5,
  │     "vector": [0.12, 0.34, ...],
  │     "k": 10,
  │     "ef_search": 128
  │   }
  └─► Set timeout: 50ms

Fan-out: 16 parallel requests (one per shard)

Step 6: Worker Search Execution
───────────────────────────────
Worker 2 (Shard 5):
  ├─► Validate shard lease (am I still leader?)
  ├─► Acquire HNSW read lock (RLock)
  ├─► Execute HNSW search:
  │
  │   Algorithm:
  │   1. Start at entry point (top layer)
  │   2. Greedy traverse to closest node at each level
  │   3. Use as entry for next level down
  │   4. At layer 0, maintain candidate set of size ef_search
  │   5. Return top_k closest vectors
  │
  ├─► For each candidate:
  │     ├─► Compute distance using AVX2 SIMD
  │     └─► Quantized comparison (int8)
  ├─► Release HNSW read lock
  ├─► Return: [vec_042, vec_107, vec_203, ...] with scores
  └─► Local results: 10 vectors

Timing per worker: 2-4ms

Step 7: Result Aggregation
──────────────────────────
API Gateway receives 16 result sets:
  ├─► Shard 1:  [v5:0.92, v12:0.89, ...]
  ├─► Shard 2:  [v203:0.95, v45:0.91, ...]
  ├─► Shard 3:  [v892:0.88, ...]
  ...
  └─► Shard 16: [v56:0.93, ...]

Merge using Min-Heap (size k=10):
  1. Insert all vectors into heap ordered by score
  2. Keep only top 10 globally
  3. O(n log k) where n=16*10=160, k=10

Result: Global top 10 vectors

Step 8: Optional Reranking
──────────────────────────
If reranker enabled:
  API Gateway ──► Reranker Service
  
  Request:
    {
      "query": "optional text",
      "candidates": [
        {"id": "v203", "score": 0.95, "metadata": {...}},
        ...
      ]
    }
  
  Reranker:
    ├─► Check cache (query + candidate IDs)
    ├─► Apply rules:
    │     - Metadata boosting
    │     - TF-IDF keyword matching
    │     - Business logic rules
    ├─► Reorder candidates
    └─► Return reranked results

Step 9: Cache Population
────────────────────────
API Gateway:
  ├─► Store results in TinyLFU cache
  ├─► TTL: 200ms (configurable)
  └─► Admit only if frequency > threshold

Step 10: Response
─────────────────
API Gateway ──► Client

Response:
{
  "results": [
    {"id": "v203", "score": 0.95, "payload": {...}},
    {"id": "v56", "score": 0.93, "payload": {...}},
    {"id": "v42", "score": 0.92, "payload": {...}},
    ... (10 results)
  ]
}

Timing Breakdown (Cache Miss):
  Gateway processing: 1ms
  Shard resolution: 0.5ms
  Parallel search (max): 4ms
  Result aggregation: 0.5ms
  Reranking (optional): 2ms
  Total: 8ms (P50), 12ms (P99)
```

---

## 4. Control Flow Analysis

### 4.1 Worker Registration Flow

```
┌──────────────────────────────────────────────────────────────────────┐
│                    WORKER REGISTRATION FLOW                          │
└──────────────────────────────────────────────────────────────────────┘

Phase 1: Worker Startup
───────────────────────
Worker Node (New):
  ├─► Load configuration (env vars / config file)
  ├─► Initialize gRPC server
  ├─► Initialize Raft address
  └─► Connect to PD cluster

Phase 2: Registration Request
─────────────────────────────
Worker ──► Placement Driver (gRPC)

RegisterWorker Request:
{
  "grpc_address": "10.0.1.15:6201",
  "raft_address": "10.0.1.15:6301",
  "cpu_cores": 24,
  "memory_bytes": 274877906944,  // 256GB
  "disk_bytes": 1099511627776000, // 1TB
  "rack": "rack-03",
  "zone": "us-east-1a",
  "region": "us-east-1",
  "capabilities": ["search-optimized"]
}

Phase 3: Raft Consensus
───────────────────────
Placement Driver Leader:
  ├─► Receive RegisterWorker request
  ├─► Propose command to Raft group
  ├─► Replicate to PD followers
  ├─► Wait for majority commit
  └─► Apply to FSM

FSM Apply:
  ├─► Generate WorkerID (monotonic counter)
  ├─► Calculate capacity score:
  │   cpu_score = (24/8) * 300 = 900
  │   mem_score = (256GB/32GB) * 400 = 3200
  │   disk_score = (1TB/500GB) * 300 = 600
  │   total_capacity = 900 + 3200 + 600 = 4700
  ├─► Store WorkerInfo:
  │   {
  │     "id": 42,
  │     "addresses": {...},
  │     "capacity": 4700,
  │     "failure_domain": {"rack":"03", "zone":"us-east-1a", ...},
  │     "state": "JOINING",
  │     "last_heartbeat": 1705000000
  │   }
  └─► Update topology

Phase 4: Assignment
───────────────────
Placement Driver:
  ├─► Check for under-replicated shards
  ├─► Check for load imbalance
  ├─► Assign shards to new worker (if needed)
  └─► Return response

RegisterWorker Response:
{
  "worker_id": "42",
  "assigned_shards": [15, 23, 41],  // If any under-replicated
  "assignments_epoch": 1523
}

Phase 5: Continuous Heartbeat
─────────────────────────────
Every 5 seconds:
  Worker ──► PD: Heartbeat Request
  {
    "worker_id": "42",
    "timestamp": 1705000300,
    "metrics": {
      "cpu_usage": 45.2,
      "memory_usage": 62.1,
      "qps": 15234,
      "active_shards": 8
    }
  }

  PD ──► Worker: Heartbeat Response
  {
    "ok": true,
    "assignments_epoch": 1523,
    "config_changes": []  // If any new assignments
  }

Failure Detection:
  ├─► If heartbeat missed for 15s: Mark UNHEALTHY
  ├─► Trigger failover for all shards on worker
  └─► Re-replicate to restore replication factor
```

### 4.2 Shard Rebalancing Flow

```
┌──────────────────────────────────────────────────────────────────────┐
│                    SHARD REBALANCING FLOW                            │
└──────────────────────────────────────────────────────────────────────┘

Trigger: PD Reconciler detects imbalance

Scenario 1: Hot Shard Detection
────────────────────────────────
PD Metrics:
  Worker 1: CPU 85%, 5 shards, QPS 45,000
  Worker 2: CPU 30%, 5 shards, QPS 12,000
  Worker 3: CPU 40%, 5 shards, QPS 15,000

Shard 12 on Worker 1:
  - Receives 60% of queries
  - CPU contribution: 50% of worker load
  - Classified as "HOT SHARD"

Rebalancing Decision:
  ├─► Move Shard 12 replica to Worker 2
  └─► Update routing to split queries

Scenario 2: Capacity-Based Rebalancing
───────────────────────────────────────
New Worker 4 joins (64 cores, 512GB RAM)
Current distribution:
  Workers 1-3: 15 shards each (overloaded)
  Worker 4: 0 shards

Rebalancing:
  ├─► Move 5 shards from Worker 1 to Worker 4
  ├─► Move 5 shards from Worker 2 to Worker 4
  ├─► Move 5 shards from Worker 3 to Worker 4
  └─► Final: All workers have 10 shards

Execution Flow:
────────────────
Step 1: PD Decision
  PD Leader proposes MoveShard:
  {
    "shard_id": 12,
    "source_worker": 1,
    "target_worker": 2
  }

Step 2: Raft Commit
  PD replicates command to followers
  Committed on majority

Step 3: Target Preparation
  PD notifies Worker 2:
  "Add replica for shard 12"
  
  Worker 2:
  ├─► Create new PebbleDB instance
  ├─► Initialize HNSW index structure
  ├─► Join shard's Raft group as learner (non-voting)
  └─► Request snapshot from current leader

Step 4: Data Streaming
  Worker 1 (Leader) ──► Worker 2 (New Replica)
  
  Phase A: Snapshot Transfer
    ├─► Worker 1 creates PebbleDB snapshot
    ├─► Compress and stream to Worker 2
    ├─► Worker 2 restores snapshot
    └─► Time: 2-5 minutes for 10M vectors
  
  Phase B: WAL Catch-up
    ├─► Worker 2 receives updates since snapshot
    ├─► Applies to local PebbleDB
    ├─► HNSW indexing (async)
    └─► Time: 10-30 seconds

Step 5: Promotion
  When Worker 2 is caught up:
  ├─► PD promotes Worker 2 to voter
  ├─► Worker 2 participates in consensus
  └─► Now has full replica

Step 6: Query Routing Update
  PD updates routing table
  Gateway cache expires (2s TTL)
  New queries distributed to both replicas

Step 7: Source Cleanup (Optional)
  If reducing replication factor:
  ├─► PD removes Worker 1 from replica set
  ├─► Worker 1 deletes local shard data
  └─► Resources freed

Timeline:
  Decision: 0ms
  Preparation: 100ms
  Snapshot transfer: 3 minutes
  Catch-up: 20 seconds
  Promotion: 50ms
  Total: ~3.5 minutes for large shard
```

---

## 5. Component Interactions

### 5.1 Inter-Service Communication Matrix

```
┌──────────────────────────────────────────────────────────────────────┐
│               INTER-SERVICE COMMUNICATION MATRIX                     │
└──────────────────────────────────────────────────────────────────────┘

                    │ Gateway │    PD    │  Worker  │   Auth   │ Reranker │
────────────────────┼─────────┼──────────┼──────────┼──────────┼──────────┤
API Gateway         │    -    │  gRPC    │  gRPC    │  gRPC    │  gRPC    │
  ├─ Purpose        │         │ Metadata │  Data    │ Validate │ Rerank   │
  ├─ Frequency      │         │  Low     │  High    │  Medium  │  Medium  │
  ├─ Caching        │         │  2s TTL  │  N/A     │  60s TTL │  N/A     │
  └─ Timeout        │         │  5s      │  50ms    │  100ms   │  100ms   │
────────────────────┼─────────┼──────────┼──────────┼──────────┼──────────┤
Placement Driver    │   -     │ Raft     │  gRPC    │    -     │    -     │
  ├─ Purpose        │         │ Consensus│ Assign   │          │          │
  ├─ Frequency      │         │  High    │  Low     │          │          │
  └─ Pattern        │         │ Internal │ Heartbeat│          │          │
────────────────────┼─────────┼──────────┼──────────┼──────────┼──────────┤
Worker              │   -     │    -     │ Raft     │    -     │    -     │
  ├─ Purpose        │         │          │ Consensus│          │          │
  ├─ Frequency      │         │          │  High    │          │          │
  └─ Pattern        │         │          │ Per-shard│          │          │
────────────────────┼─────────┼──────────┼──────────┼──────────┼──────────┤
Auth Service        │   -     │    -     │    -     │    -     │    -     │
  ├─ Purpose        │         │          │          │ Internal │          │
  └─ Storage        │         │          │          │  etcd    │          │
────────────────────┼─────────┼──────────┼──────────┼──────────┼──────────┤

Communication Patterns:
───────────────────────
1. Request-Response (Sync):
   Gateway ◄──► PD: Get routing info
   Gateway ◄──► Worker: Search/Insert
   Gateway ◄──► Auth: Validate token

2. Streaming (Async):
   Worker ───► Worker: WAL replication
   Worker ───► Search-only: Update stream

3. Pub/Sub:
   PD ───► All: Configuration changes
   Worker ───► PD: Heartbeats

4. Consensus:
   PD nodes ◄──► PD nodes: Raft
   Worker nodes ◄──► Worker nodes: Raft (per-shard)
```

### 5.2 Protocol Buffer Service Definitions

```protobuf
// API Gateway Public API
service VectronService {
  rpc CreateCollection(CreateCollectionRequest) returns (CreateCollectionResponse);
  rpc DeleteCollection(DeleteCollectionRequest) returns (DeleteCollectionResponse);
  rpc Upsert(UpsertRequest) returns (UpsertResponse);
  rpc Search(SearchRequest) returns (SearchResponse);
  rpc SearchStream(SearchRequest) returns (stream SearchResponse);
  rpc Get(GetRequest) returns (GetResponse);
  rpc Delete(DeleteRequest) returns (DeleteResponse);
}

// Placement Driver Internal API
service PlacementService {
  rpc RegisterWorker(RegisterWorkerRequest) returns (RegisterWorkerResponse);
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
  rpc GetWorker(GetWorkerRequest) returns (GetWorkerResponse);
  rpc CreateCollection(CreateCollectionRequest) returns (CreateCollectionResponse);
  rpc ListCollections(ListCollectionsRequest) returns (ListCollectionsResponse);
}

// Worker Internal API
service WorkerService {
  rpc StoreVector(StoreVectorRequest) returns (StoreVectorResponse);
  rpc BatchStoreVector(BatchStoreVectorRequest) returns (BatchStoreVectorResponse);
  rpc Search(SearchRequest) returns (SearchResponse);
  rpc BatchSearch(BatchSearchRequest) returns (BatchSearchResponse);
  rpc StreamHNSWSnapshot(HNSWSnapshotRequest) returns (stream HNSWSnapshotChunk);
}
```

---

## 6. State Machines

### 6.1 Worker Lifecycle State Machine

```
┌──────────────────────────────────────────────────────────────────────┐
│                    WORKER LIFECYCLE STATES                           │
└──────────────────────────────────────────────────────────────────────┘

                            ┌──────────┐
                            │  START   │
                            └────┬─────┘
                                 │
                                 ▼ Initialize
                            ┌──────────┐
                            │ JOINING  │
                            └────┬─────┘
                                 │ Register with PD
                                 │ (Raft commit)
                                 ▼
         ┌──────────────────┬──────────┬──────────────────┐
         │                  │          │                  │
   Healthy            Assigned     No capacity        Registration
   heartbeat          shards       needed             failed
         │                  │          │                  │
         ▼                  ▼          ▼                  ▼
   ┌──────────┐      ┌──────────┐  ┌──────────┐    ┌──────────┐
   │  READY   │      │ ACTIVE   │  │ STANDBY  │    │  ERROR   │
   └────┬─────┘      └────┬─────┘  └────┬─────┘    └────┬─────┘
        │                 │             │                │
        │ Heartbeat       │ Processing  │ Waiting for    │ Retry
        │ missed          │ queries     │ assignment     │ (exponential)
        │ (15s)           │             │                │
        ▼                 │             │                ▼
   ┌──────────┐           │             │           ┌──────────┐
   │UNHEALTHY │◄──────────┼─────────────┼───────────│  RETRY   │
   └────┬─────┘           │             │           └──────────┘
        │                 │             │
        │ Failover        │             │
        │ initiated       │             │
        ▼                 │             │
   ┌──────────┐           │             │
   │ DRAINING │◄──────────┘             │
   └────┬─────┘   Move shards away      │
        │                                │
        │ All shards                     │
        │ moved                          │
        ▼                                │
   ┌──────────┐                         │
   │  REMOVED │◄─────────────────────────┘
   └──────────┘  Can re-register as new worker

State Descriptions:
───────────────────
JOINING: Initial registration in progress
READY: Healthy, waiting for shard assignments
ACTIVE: Hosting shards and processing queries
STANDBY: Healthy but no shards assigned (spare capacity)
UNHEALTHY: Heartbeats missed, failover initiated
DRAINING: Shards being moved away before removal
ERROR: Registration or initialization failed
REMOVED: Cleanly removed from cluster
```

### 6.2 Shard State Machine (Per Replica)

```
┌──────────────────────────────────────────────────────────────────────┐
│                    SHARD REPLICA STATES                              │
└──────────────────────────────────────────────────────────────────────┘

                       ┌──────────────┐
                       │  CREATING    │
                       │ (Pebble init)│
                       └──────┬───────┘
                              │
                              ▼ Join Raft group
                       ┌──────────────┐
                       │   LEARNER    │
                       │(Non-voting)  │
                       └──────┬───────┘
                              │
             ┌────────────────┼────────────────┐
             │                │                │
       Caught up        Snapshot          Follower
       to leader       transfer          promotion
             │          complete             │
             ▼                │                ▼
       ┌──────────┐           │          ┌──────────┐
       │CANDIDATE │           │          │ FOLLOWER │
       │(Voting)  │◄──────────┴─────────►│          │
       └────┬─────┘                      └────┬─────┘
            │                                 │
            │ Leader                          │ Leader
            │ election                        │ timeout
            │ wins                            │ (no heartbeat)
            ▼                                 ▼
       ┌──────────┐                      ┌──────────┐
       │  LEADER  │                      │CANDIDATE │
       │(Primary) │                      │(Election)│
       └────┬─────┘                      └──────────┘
            │
            │ Step down
            │ (higher term)
            ▼
       ┌──────────┐
       │ FOLLOWER │
       └──────────┘

State Transitions:
──────────────────
CREATING → LEARNER: Initialize DB and join Raft
LEARNER → CANDIDATE: Caught up, promoted to voter
CANDIDATE → LEADER: Won election
CANDIDATE → FOLLOWER: Lost election
FOLLOWER → CANDIDATE: Leader timeout, start election
LEADER → FOLLOWER: Received message with higher term
LEADER/FOLLOWER → LEARNER: Demoted, re-syncing
```

---

## 7. Failure Scenarios

### 7.1 Worker Node Failure

```
┌──────────────────────────────────────────────────────────────────────┐
│                    WORKER FAILURE RECOVERY                           │
└──────────────────────────────────────────────────────────────────────┘

Timeline:
─────────
T+0s:     Worker 2 crashes (OOM, hardware failure, etc.)
T+5s:     PD misses first heartbeat
T+10s:    PD misses second heartbeat
T+15s:    PD marks Worker 2 as UNHEALTHY
          ├─► Triggers failover for all shards on Worker 2
          └─► Shards: [5, 12, 23]

For each affected shard:

Shard 5 (3 replicas: Worker 1, 2, 4):
─────────────────────────────────────
Current state:
  Worker 1: LEADER (was following Worker 2)
  Worker 2: CRASHED (was leader for some shards)
  Worker 4: FOLLOWER

Raft Behavior:
  ├─► Worker 1 times out (no heartbeat from leader)
  ├─► Worker 1 increments term
  ├─► Worker 1 requests votes from Worker 4
  ├─► Worker 4 grants vote (log is up to date)
  ├─► Worker 1 becomes LEADER
  └─► Service continues with 2 of 3 replicas

PD Actions:
  ├─► Detects Worker 2 failure
  ├─► Identifies under-replicated shards
  ├─► Selects replacement worker (Worker 5)
  └─► Initiates re-replication

Worker 5 (New Replica):
  ├─► Joins Shard 5 as LEARNER
  ├─► Streams snapshot from Worker 1 (leader)
  ├─► Catches up via WAL
  ├─► Promoted to voter
  └─► Full replication restored (3 of 3)

Total Recovery Time:
  Failure detection: 15s
  Leader election: 1s
  New replica creation: 3-5 minutes
  
  Query availability: 16s (leader election only)
  Full durability: 3-5 minutes (re-replication)

Data Loss: 0 (Raft durability guarantee)
```

### 7.2 Network Partition

```
┌──────────────────────────────────────────────────────────────────────┐
│                    NETWORK PARTITION HANDLING                        │
└──────────────────────────────────────────────────────────────────────┘

Scenario: AZ failure splits cluster

Before Partition:
─────────────────
Cluster: 5 nodes (PD), 12 workers
  AZ-1 (us-east-1a): PD-1, PD-2, Workers 1-4
  AZ-2 (us-east-1b): PD-3, Workers 5-8
  AZ-3 (us-east-1c): PD-4, PD-5, Workers 9-12

Partition Event:
────────────────
Network link between AZ-1 and AZ-2+3 fails

Partition View:
  Partition A (Majority): AZ-1
    PD-1, PD-2 (2 of 5 PD nodes)
    Workers 1-4
    
  Partition B (Minority): AZ-2+3
    PD-3, PD-4, PD-5 (3 of 5 PD nodes) ← MAJORITY
    Workers 5-12

Placement Driver Behavior:
──────────────────────────
Partition B (Majority):
  ├─► 3 nodes can form quorum (3 > 5/2)
  ├─► Continue serving metadata operations
  ├─► Mark workers in Partition A as UNHEALTHY
  ├─► Trigger re-replication to workers in Partition B
  └─► System continues operating

Partition A (Minority):
  ├─► 2 nodes cannot form quorum
  ├─► Stop accepting metadata writes
  ├─► PD operations fail (as designed)
  └─► Workers continue serving reads from cache

Shard 5 (Replicas: Worker 1 in A, Worker 6 in B, Worker 9 in B):
────────────────────────────────────────────────────────────────
Worker 6 (Partition B):
  ├─► Can communicate with Worker 9
  ├─► Forms majority (2 of 3)
  ├─► Continue accepting writes
  └─► Elects new leader if needed

Worker 1 (Partition A):
  ├─► Cannot reach other replicas
  ├─► Steps down if was leader
  ├─► Stops accepting writes
  └─► Serves stale reads only

When Partition Heals:
─────────────────────
  ├─► Worker 1 reconnects
  ├─► Raft log reconciliation
  ├─► Worker 1 applies missed writes
  ├─► Rejoins as follower
  └─► System fully restored

Data Consistency:
  ├─► Partition B (majority): All writes committed
  ├─► Partition A (minority): No writes committed during partition
  ├─► After heal: All nodes converge to same state
  └─► No data loss, no split-brain
```

### 7.3 Placement Driver Leader Failure

```
┌──────────────────────────────────────────────────────────────────────┐
│                    PD LEADER FAILURE RECOVERY                        │
└──────────────────────────────────────────────────────────────────────┘

Initial State:
──────────────
PD Cluster: 5 nodes
  PD-1: LEADER
  PD-2: FOLLOWER
  PD-3: FOLLOWER
  PD-4: FOLLOWER
  PD-5: FOLLOWER

Failure Event:
──────────────
PD-1 crashes

Detection (Followers):
──────────────────────
Each follower maintains election timer:
  Election timeout: 1-2s (randomized)
  
PD-2 timeout fires first:
  ├─► Increment term: 5 → 6
  ├─► Transition to CANDIDATE
  ├─► Request votes from PD-3, PD-4, PD-5
  │
  ├─► PD-3: Grant vote (log up to date)
  ├─► PD-4: Grant vote
  ├─► PD-5: Grant vote
  │
  ├─► Received 3 votes (majority of 5)
  ├─► Transition to LEADER
  ├─► Send heartbeats to all followers
  └─► Resume serving requests

Total Downtime:
───────────────
  Detection: 1-2s
  Election: 0.5s
  Total: <2s

Impact:
───────
  Metadata operations: Unavailable for 2s
  Data operations (search/insert): Continue normally
    ├─► Gateway uses cached routing
    ├─► Workers serve requests
    └─► No user-visible impact

Client Experience:
──────────────────
  During 2s window:
    ├─► New collection creation: Timeout (rare operation)
    ├─► Search queries: Success (cached routing)
    ├─► Insert operations: Success (cached routing)
    └─► No retries needed for data operations
```

---

## 8. Performance Characteristics

### 8.1 Latency Breakdown

```
┌──────────────────────────────────────────────────────────────────────┐
│                    LATENCY BREAKDOWN                                 │
└──────────────────────────────────────────────────────────────────────┘

WRITE OPERATION (Upsert 100 vectors)
─────────────────────────────────────
├─ Gateway Processing      ████░░░░░░░░░░░░░░░░░░  1.5ms (10%)
│   ├─ Auth validation     ██░░░░░░░░░░░░░░░░░░░░  0.5ms
│   ├─ Routing             █░░░░░░░░░░░░░░░░░░░░░  0.3ms
│   └─ Batch preparation   █░░░░░░░░░░░░░░░░░░░░░  0.7ms
│
├─ Network (RTT)           ███░░░░░░░░░░░░░░░░░░░  1.0ms (7%)
│
├─ Raft Consensus          ██████████████░░░░░░░░  4.5ms (30%)
│   ├─ Leader append       ███░░░░░░░░░░░░░░░░░░░  1.0ms
│   ├─ Replication         ██████░░░░░░░░░░░░░░░░  2.0ms
│   ├─ Majority ack        ███░░░░░░░░░░░░░░░░░░░  1.0ms
│   └─ Commit              █░░░░░░░░░░░░░░░░░░░░░  0.5ms
│
├─ PebbleDB Write          ██████░░░░░░░░░░░░░░░░  2.0ms (13%)
│
├─ HNSW Indexing (async)   ████████████████████░░  5.0ms (33%) [BG]
│   └─ Not blocking response
│
└─ Total Ack Time          ███████████████░░░░░░░  9.0ms

READ OPERATION (Search top-10)
──────────────────────────────
├─ Gateway Cache Check     █░░░░░░░░░░░░░░░░░░░░░  0.5ms (6%)
│   └─ HIT: Return immediately
│   └─ MISS: Continue
│
├─ Shard Resolution        ░░░░░░░░░░░░░░░░░░░░░░  0.1ms (1%)
│   └─ Cached
│
├─ Parallel Fan-out        ██████████████████░░░░  4.0ms (50%)
│   ├─ 16 parallel requests
│   ├─ Network (RTT)       █░░░░░░░░░░░░░░░░░░░░░  0.5ms
│   ├─ Worker processing   ████████████████░░░░░░  3.0ms
│   │   ├─ HNSW search     ████████████░░░░░░░░░░  2.0ms
│   │   │   ├─ Graph nav   ██████░░░░░░░░░░░░░░░░  1.0ms
│   │   │   └─ Distance    ████░░░░░░░░░░░░░░░░░░  0.8ms (SIMD)
│   │   └─ Result prep     ████░░░░░░░░░░░░░░░░░░  0.5ms
│   └─ Network return      █░░░░░░░░░░░░░░░░░░░░░  0.5ms
│
├─ Result Aggregation      ██░░░░░░░░░░░░░░░░░░░░  0.5ms (6%)
│   └─ Min-heap merge
│
├─ Cache Population        █░░░░░░░░░░░░░░░░░░░░░  0.3ms (4%)
│
└─ Total (Cache Miss)      ████████████████████░░  8.0ms
   Total (Cache Hit)       █░░░░░░░░░░░░░░░░░░░░░  0.5ms

P99 vs P50 Breakdown:
─────────────────────
P50 Search:  3.2ms (all local, cache hit)
P95 Search:  6.1ms (1 slow worker, network delay)
P99 Search:  8.9ms (GC pause, queueing, tail latency)
P99.9:       15ms (outlier, hardware interrupt)
```

### 8.2 Throughput Scaling

```
┌──────────────────────────────────────────────────────────────────────┐
│                    THROUGHPUT SCALING                                │
└──────────────────────────────────────────────────────────────────────┘

Single Worker Capacity:
───────────────────────
CPU: 24 cores
├─ Search operations: 52,000 QPS
│   └─ CPU per query: 0.46ms × 24 cores = 11ms CPU/query
├─ Write operations: 8,000 TPS
│   └─ Limited by Raft consensus latency
└─ Mixed (70/30): 35,000 QPS

Cluster Scaling:
────────────────
Workers  │ Shards │ Total QPS │ Efficiency
─────────┼────────┼───────────┼──────────
1        │ 4      │ 52,000    │ 100%
3        │ 16     │ 156,000   │ 100%
6        │ 32     │ 312,000   │ 100%
12       │ 64     │ 624,000   │ 99%
24       │ 128    │ 1,200,000 │ 96%
48       │ 256    │ 2,200,000 │ 88%

Scaling Limitations:
────────────────────
1. Gateway bottleneck:
   └─ Single gateway: 100,000 QPS limit
   └─ Solution: Multiple gateways + load balancer

2. PD metadata queries:
   └─ 50,000 routing lookups/second
   └─ Solution: Gateway caching (2s TTL)

3. Network bandwidth:
   └─ 25Gbps = 3.1GB/s
   └─ Per query: 768 dims × 4 bytes = 3KB
   └─ Limit: 1M QPS per node

4. Coordination overhead:
   └─ Logarithmic with cluster size
   └─ P99 latency increases 0.1ms per 2× workers
```

---

## 9. Deployment Architecture

### 9.1 Production Deployment Topology

```
┌──────────────────────────────────────────────────────────────────────┐
│                    PRODUCTION DEPLOYMENT                             │
│                         (Multi-AZ, HA)                               │
└──────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│                         Availability Zone 1                         │
│                              (us-east-1a)                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌──────────────────────┐  ┌──────────────────────┐               │
│  │   API Gateway x2     │  │   Auth Service x2    │               │
│  │   (Load Balanced)    │  │   (Active-Active)    │               │
│  │                      │  │                      │               │
│  │  - 16 vCPU, 32GB     │  │  - 8 vCPU, 16GB      │               │
│  │  - 2 nodes           │  │  - etcd cluster      │               │
│  └──────────────────────┘  └──────────────────────┘               │
│                                                                     │
│  ┌──────────────────────┐  ┌──────────────────────┐               │
│  │  Placement Driver    │  │   Reranker x2        │               │
│  │  (Raft Cluster)      │  │   (Optional)         │               │
│  │                      │  │                      │               │
│  │  - 3 nodes (1,2,3)   │  │  - Redis cache       │               │
│  │  - 8 vCPU, 16GB each │  │                      │               │
│  └──────────────────────┘  └──────────────────────┘               │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │                    Worker Nodes x4                            │ │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐ │
│  │  │ Worker 1   │ │ Worker 2   │ │ Worker 3   │ │ Worker 4   │ │
│  │  │ - 24 vCPU  │ │ - 24 vCPU  │ │ - 24 vCPU  │ │ - 24 vCPU  │ │
│  │  │ - 256GB    │ │ - 256GB    │ │ - 256GB    │ │ - 256GB    │ │
│  │  │ - 2TB SSD  │ │ - 2TB SSD  │ │ - 2TB SSD  │ │ - 2TB SSD  │ │
│  │  │ - 20 shards│ │ - 20 shards│ │ - 20 shards│ │ - 20 shards│ │
│  │  └────────────┘ └────────────┘ └────────────┘ └────────────┘ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                                  │
                                  │ Cross-AZ Links (10Gbps)
                                  │ Latency: 1-2ms
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         Availability Zone 2                         │
│                              (us-east-1b)                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌──────────────────────┐  ┌──────────────────────────────────────┐│
│  │   API Gateway x2     │  │         Worker Nodes x4              ││
│  │   (Load Balanced)    │  │  ┌────────┐┌────────┐┌────────┐┌────┐││
│  └──────────────────────┘  │  │Worker 5││Worker 6││Worker 7││W8  │││
│                           │  │-24 core││-24 core││-24 core││... │││
│  ┌──────────────────────┐  │  │-256GB ││-256GB ││-256GB ││... │││
│  │  Placement Driver    │  │  │-20 shd││-20 shd││-20 shd││... │││
│  │  (Nodes 4,5)         │  │  └────────┘└────────┘└────────┘└────┘││
│  └──────────────────────┘  └──────────────────────────────────────┘│
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         Availability Zone 3                         │
│                              (us-east-1c)                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │                    Worker Nodes x4                            │ │
│  │         (Search-only nodes + Standard workers)               │ │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐ │
│  │  │ Worker 9   │ │ Worker 10  │ │ Worker 11  │ │ Worker 12  │ │
│  │  │ (Search)   │ │ (Search)   │ │ (Standard) │ │ (Standard) │ │
│  │  │ - No Raft  │ │ - No Raft  │ │ - Full     │ │ - Full     │ │
│  │  │ - 48 vCPU  │ │ - 48 vCPU  │ │ - 24 vCPU  │ │ - 24 vCPU  │ │
│  │  │ - 512GB    │ │ - 512GB    │ │ - 256GB    │ │ - 256GB    │ │
│  │  │ - Hot cache│ │ - Hot cache│ │            │ │            │ │
│  │  └────────────┘ └────────────┘ └────────────┘ └────────────┘ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘

Supporting Infrastructure:
──────────────────────────

┌─────────────────────────────────────────────────────────────────┐
│                     Redis Cluster                               │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐                │
│  │ Node 1     │  │ Node 2     │  │ Node 3     │                │
│  │ (Master)   │  │ (Master)   │  │ (Master)   │                │
│  │ - 16GB     │  │ - 16GB     │  │ - 16GB     │                │
│  └────────────┘  └────────────┘  └────────────┘                │
│       │               │               │                         │
│       ▼               ▼               ▼                         │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐                │
│  │ Replica    │  │ Replica    │  │ Replica    │                │
│  └────────────┘  └────────────┘  └────────────┘                │
│                                                                 │
│  Usage: Cross-gateway cache, Session store, Rate limiting      │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                     etcd Cluster                                │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐                │
│  │ Node 1     │  │ Node 2     │  │ Node 3     │                │
│  │ (Auth Svc) │  │ (Auth Svc) │  │ (Auth Svc) │                │
│  │ - 4GB      │  │ - 4GB      │  │ - 4GB      │                │
│  └────────────┘  └────────────┘  └────────────┘                │
│                                                                 │
│  Usage: User credentials, API keys, Auth state                 │
└─────────────────────────────────────────────────────────────────┘

Network Architecture:
─────────────────────
├─ Internet-facing LB (Layer 7)
├─ Internal LB for services
├─ Service mesh (optional)
├─ VPC peering between AZs
└─ Security groups restricting traffic

Total Resources:
────────────────
├─ API Gateways: 4 nodes (2 per AZ)
├─ PD Cluster: 5 nodes (distributed)
├─ Workers: 12 nodes (4 per AZ)
├─ Auth/etcd: 3 nodes
├─ Redis: 6 nodes (3 master + 3 replica)
└─ Total: ~30 nodes for full HA deployment

Capacity:
─────────
├─ Storage: 12 workers × 256GB × 0.75 (quantization) = 2.3TB usable
├─ QPS: 12 workers × 52,000 = 624,000 queries/second
├─ Concurrent connections: 100,000+
└─ Fault tolerance: Can lose 1 AZ + 1 worker per shard
```

---

## Summary

Vectron's architecture represents a comprehensive approach to distributed vector search, combining:

1. **Strong Consistency**: Via Raft consensus for metadata and per-shard replication
2. **High Performance**: Through SIMD acceleration, quantization, and multi-tier caching
3. **Fault Tolerance**: With automatic failover, re-replication, and AZ awareness
4. **Operational Simplicity**: Via automated rebalancing, self-healing, and clear failure modes
5. **Horizontal Scalability**: Supporting linear scaling to hundreds of nodes

The system is designed for production deployments requiring billion-scale vector storage, sub-10ms query latency, and 99.99% availability.
