# Vectron Development Progress

This file tracks the major development progress of the Vectron distributed vector database.

## Phase 1: Placement Driver Refactoring (Completed)

The initial version of the `placementdriver` has been completely refactored to serve as the central brain of the distributed system.

**Key achievements:**
-   **Raft Engine**: Replaced `hashicorp/raft` with `dragonboat/raft`, setting the foundation for a multi-group Raft system. The PD now runs as a static, multi-node Raft cluster.
-   **Global Shard Map**: The PD's state machine (FSM) has been redesigned to manage a global map of collections and their shards.
-   **Initial Sharding**: The `CreateCollection` RPC now automatically splits a new collection into a predefined number of range-based shards.
-   **Replica Management**: For each shard, the PD assigns a set of worker nodes as replicas (replication factor = 3).
-   **"Watch" API (via Heartbeat)**: A mechanism for workers to discover which shards they are responsible for has been implemented by piggybacking shard information on the `Heartbeat` RPC response.

The `placementdriver` is now ready to orchestrate the worker nodes.

## Phase 2: Worker Node Overhaul (In Progress)

The next major phase is to refactor the `worker` node to operate as a stateful, dynamic participant in the distributed database.

**The plan includes:**
1.  **Integrate Dragonboat**: Embed a `dragonboat.NodeHost` into the worker process. This will allow a single worker to participate in hundreds of shard-specific Raft groups simultaneously.
2.  **Implement PD Client**: The worker will run a background process to periodically heartbeat to the PD and receive its current shard assignments.
3.  **Dynamic Shard Management**: The worker will compare its current set of running shard replicas with the assignments from the PD, and will dynamically join new shard Raft groups or leave old ones.
4.  **Per-Shard Storage**: For each shard replica it manages, the worker will maintain a separate Pebble DB instance and HNSW index under a `./data/<collection>/<shard_id>` directory structure.
