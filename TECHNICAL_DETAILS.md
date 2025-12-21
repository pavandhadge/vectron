# Vectron Technical Details

This document provides a deeper dive into the architecture and technical details of the Vectron distributed vector database.

## Core Concepts

### Collections and Shards

A **Collection** is the top-level container for vectors. Each collection is partitioned into multiple **Shards**. A shard is a horizontal slice of a collection, responsible for a specific range of the key space. This sharding allows Vectron to scale horizontally by distributing the data and workload across multiple worker nodes.

### Raft Consensus

Vectron uses the [Raft consensus algorithm](https://raft.github.io/) to ensure consistency and fault tolerance. There are two levels of Raft consensus in Vectron:

1.  **`placementdriver` Cluster:** The `placementdriver` nodes form a Raft cluster to manage the global state of the system. This includes the list of worker nodes, collection metadata, and the assignment of shards to workers.
2.  **Per-Shard Replication:** Each shard is itself a Raft cluster. The replicas of a shard, which are located on different worker nodes, use Raft to replicate write operations. This ensures that the data within each shard is consistent and durable.

## Service Deep Dive

### `apigateway`

*   **Role:** The `apigateway` is the stateless entry point for all client requests. It is responsible for:
    *   Authentication and authorization.
    *   Serving both gRPC and HTTP/JSON traffic.
    *   Forwarding client requests to the appropriate `worker` node.
*   **Request Flow:**
    1.  A client sends a request (e.g., `Upsert`, `Search`) to the `apigateway`.
    2.  The `apigateway` authenticates the request.
    3.  It queries the `placementdriver` to determine which worker node is the leader for the shard that the request is targeting.
    4.  It forwards the request to the leader `worker` node.
    5.  It receives the response from the `worker` and sends it back to the client.

### `placementdriver`

*   **Role:** The `placementdriver` is the brain of the cluster. It is a distributed, fault-tolerant service that manages the cluster's metadata. Its responsibilities include:
    *   **Worker Management:** Keeping track of all the `worker` nodes in the cluster and their health via heartbeats.
    *   **Collection and Shard Management:** Managing the creation and schema of collections, and the partitioning of collections into shards.
    *   **Shard Placement:** Assigning shard replicas to `worker` nodes and ensuring that the replication factor is maintained.
    *   **Leader Election:** The `placementdriver` itself doesn't elect shard leaders, but it provides the information that clients and the `apigateway` need to find the leader of a shard's Raft group.
*   **State Machine:** The `placementdriver` uses a Raft state machine to manage its state. The state includes:
    *   A list of all registered `worker` nodes.
    *   A map of all `collections` and their `shards`.
    *   For each `shard`, the list of `worker` nodes that are replicas.

### `worker`

*   **Role:** The `worker` is responsible for storing, indexing, and searching vectors. Each `worker` can host multiple shard replicas.
*   **Per-Shard Raft:** Each shard replica on a `worker` is part of a Raft consensus group for that shard. All write operations for a shard (e.g., `StoreVector`, `DeleteVector`) are sent to the leader of the shard's Raft group. The leader then replicates the command to the other replicas via the Raft log. This ensures that all replicas of a shard have the same data.
*   **Storage and Indexing:**
    *   **Storage Engine:** The `worker` uses **PebbleDB**, a RocksDB-inspired key-value store, as its underlying storage engine.
    *   **Vector Index:** For efficient similarity search, the `worker` uses a **Hierarchical Navigable Small World (HNSW)** index. The HNSW index is built on top of the data stored in PebbleDB.

## APIs

### Public API (`apigateway.proto`)

This is the API that clients use to interact with Vectron. The main RPCs are:

*   `CreateCollection`: Creates a new collection.
*   `Upsert`: Inserts or updates a vector in a collection.
*   `Search`: Searches for the most similar vectors to a query vector.
*   `Delete`: Deletes a vector from a collection.

### Internal APIs

#### `placementdriver.proto`

This API is used for communication between `worker` nodes and the `placementdriver`, and also between `placementdriver` nodes. Key RPCs include:

*   `RegisterWorker`: A `worker` calls this to join the cluster.
*   `Heartbeat`: `worker` nodes periodically send heartbeats to the `placementdriver` to signal that they are alive.
*   `GetWorker`: Used by the `apigateway` to get information about a worker.

#### `worker.proto`

This API is used for communication between the `apigateway` and `worker` nodes. The RPCs in this API mirror the public API (e.g., `Upsert`, `Search`), but are intended for internal use.
