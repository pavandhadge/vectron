# Placement Driver

The `placementdriver` is the brain of the Vectron cluster. It is a distributed, fault-tolerant service responsible for managing the cluster's metadata. It uses the Raft consensus algorithm to ensure that all nodes in the `placementdriver` cluster have a consistent view of the state.

## Features

*   **Cluster State Management:** Manages the state of the cluster, including the list of worker nodes, collection metadata, and shard assignments.
*   **Service Discovery:** Provides a mechanism for the `apigateway` to discover which `worker` node is responsible for a given shard.
*   **Worker Health Monitoring:** Keeps track of worker health via heartbeats.
*   **Collection Management:** Manages the lifecycle of collections, including creation and deletion.
*   **Fault Tolerance:** Uses the Raft consensus algorithm to provide a highly available and consistent metadata service.

## API

The `placementdriver`'s internal API is defined in `proto/placementdriver/placementdriver.proto`. It provides the following RPCs:

*   `GetWorker`: Gets the address of the worker node responsible for a given collection and optional vector ID.
*   `RegisterWorker`: Called by a `worker` node to register itself with the cluster.
*   `Heartbeat`: Called periodically by `worker` nodes to signal that they are alive.
*   `ListWorkers`: Lists all the worker nodes in the cluster.
*   `CreateCollection`: Creates a new collection.
*   `ListCollections`: Lists all collections.
*   `DeleteCollection`: Deletes a collection.

## Building

To build the `placementdriver` binary, run the following command from the root of the project:

```bash
make build-placementdriver
```

The binary will be located at `bin/placementdriver`.
