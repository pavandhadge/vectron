# Vectron Placement Driver

The `placementdriver` (PD) is the brain of the Vectron cluster. It is a distributed, fault-tolerant service responsible for managing all of the cluster's metadata. It uses the Raft consensus algorithm to ensure that all nodes in the PD cluster have a consistent, replicated view of the system's state.

## Architecture and Design

The Placement Driver is built as a distributed consensus group using the [Dragonboat](https://github.com/lni/dragonboat) Raft library. This design choice makes the PD highly available and resilient to node failures.

### 1. Raft-based Consensus

- **Replicated State Machine:** The core of the PD is a Replicated State Machine (RSM). All cluster metadata is stored in an in-memory data structure called the Finite State Machine (FSM).
- **Write Path (Proposals):** Any operation that changes the cluster state (e.g., creating a collection, registering a worker) is serialized into a `Command` and submitted to the Raft log via a `Propose` call. This command is only applied to the FSM after it has been successfully replicated to a quorum of PD nodes, guaranteeing consistency (linearizability).
- **Read Path:** Most read operations (e.g., finding a worker for a request) are served directly from the local node's in-memory FSM. This is highly performant but may not always reflect the absolute latest committed state. For reads that require guaranteed linearizability, the system uses Dragonboat's `SyncRead` mechanism.
- **Snapshotting:** To prevent the Raft log from growing indefinitely, the system periodically takes a snapshot of the FSM's in-memory state and persists it to disk. New or recovering nodes can restore their state from a recent snapshot much faster than replaying the entire log.

### 2. State Managed by the FSM

The Finite State Machine (`internal/fsm/fsm.go`) holds all the critical cluster metadata:

- **Workers:** A list of all registered `worker` nodes, including their gRPC/Raft network addresses and last heartbeat time.
- **Collections:** A list of all created vector collections. For each collection, it stores:
  - The vector dimension and distance metric.
  - A map of all shards belonging to the collection.
- **Shards:** For each shard, it stores:
  - The hash range the shard is responsible for.
  - A list of worker node IDs that host a replica of this shard's data.
  - The current leader of the shard's own Raft group, as reported by the workers.

### 3. Service Discovery (`GetWorker` RPC)

The PD's most critical role for the data plane is service discovery. When the `apigateway` receives a request for a specific vector (e.g., `Get`, `Upsert`):

1. It calls the PD's `GetWorker` RPC, providing the collection name and vector ID.
2. The PD applies a consistent hash function (FNV-64a) to the vector ID.
3. It looks up the collection and finds which shard's key range contains the resulting hash.
4. It identifies a worker node that hosts a replica of that shard (in a future implementation, it will specifically identify the shard leader).
5. It returns the gRPC address of that worker to the `apigateway`, which then forwards the request.

### 4. Worker Interaction (Registration and Heartbeats)

- **Registration:** When a `worker` node starts, it calls the `RegisterWorker` RPC to announce its presence and network addresses to the PD. The PD commits this new worker information to its FSM via a Raft proposal.
- **Heartbeats:** Workers periodically call the `Heartbeat` RPC. This serves two purposes:
  1.  **Worker -> PD:** The worker reports its health and informs the PD about the current leader status of the shards it manages.
  2.  **PD -> Worker:** The PD responds with a list of all shard replicas the worker is expected to host, including the network addresses of the other replicas in each shard's Raft group. This is how workers discover their peers to form data replication groups.

## Internal API

The PD's internal gRPC API is defined in `proto/placementdriver/placementdriver.proto`.

- `RegisterWorker`: Called by a `worker` node to register itself with the cluster.
- `Heartbeat`: Called periodically by `worker` nodes to signal that they are alive and to receive shard assignments.
- `GetWorker`: Gets the address of the worker node responsible for a given collection and vector ID.
- `ListWorkers`: Lists all the worker nodes known to the cluster.
- `CreateCollection`: Creates a new collection and its initial shards.
- `ListCollections`: Lists all collections.
- `GetCollectionStatus`: Retrieves detailed status and shard information for a collection.

## Configuration and Running

The PD is configured via command-line flags. To run a 3-node cluster, you would start three separate processes with unique node IDs and addresses.

**Example: Starting Node 1**

```bash
./bin/placementdriver \
  -node-id 1 \
  -cluster-id 100 \
  -initial-members="1:localhost:7001,2:localhost:7002,3:localhost:7003" \
  -raft-addr="localhost:7001" \
  -grpc-addr="localhost:6001" \
  -data-dir="pd-data"
```

**Example: Starting Node 2**

```bash
./bin/placementdriver \
  -node-id 2 \
  -cluster-id 100 \
  -initial-members="1:localhost:7001,2:localhost:7002,3:localhost:7003" \
  -raft-addr="localhost:7002" \
  -grpc-addr="localhost:6002" \
  -data-dir="pd-data"
```

And so on for Node 3.
