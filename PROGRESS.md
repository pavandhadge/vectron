# Vectron Development Progress

This file tracks the major development progress of the Vectron distributed vector database.

## Phase 1: Placement Driver Refactoring (Completed)

The initial version of the `placementdriver` has been completely refactored to serve as the central brain of the distributed system.

**Key achievements:**

- **Raft Engine**: Replaced `hashicorp/raft` with `dragonboat/raft`, setting the foundation for a multi-group Raft system. The PD now runs as a static, multi-node Raft cluster.
- **Global Shard Map**: The PD's state machine (FSM) has been redesigned to manage a global map of collections and their shards.
- **Initial Sharding**: The `CreateCollection` RPC now automatically splits a new collection into a predefined number of range-based shards.
- **Replica Management**: For each shard, the PD assigns a set of worker nodes as replicas (replication factor = 3).
- **"Watch" API (via Heartbeat)**: A mechanism for workers to discover which shards they are responsible for has been implemented by piggybacking shard information on the `Heartbeat` RPC response.

The `placementdriver` is now ready to orchestrate the worker nodes.

## Phase 2: Worker Node Overhaul (Completed)

This phase focused on refactoring the `worker` node to operate as a stateful, dynamic participant in the distributed database. The worker is now capable of managing multiple shard replicas and serving client requests.

**Key achievements:**

- **Dragonboat Integration**: Embedded a `dragonboat.NodeHost` into the worker process, allowing a single worker to participate in hundreds of shard-specific Raft groups simultaneously.
- **PD Client**: The worker runs a background process to periodically heartbeat to the PD and receive its current shard assignments.
- **Dynamic Shard Management**: The worker compares its current set of running shard replicas with the assignments from the PD, and dynamically joins new shard Raft groups or leaves old ones.
- **Per-Shard Storage**: For each shard replica it manages, the worker maintains a separate Pebble DB instance and HNSW index, managed by a dedicated Raft state machine.
- **Multi-Shard gRPC Server**: The worker now runs a gRPC server that can route client requests (e.g., `StoreVector`, `Search`) to the correct shard's Raft group.
- **Consistent Hashing**: The `placementdriver`'s `GetWorker` RPC has been updated to use consistent hashing on the vector ID to route clients to the correct shard.
