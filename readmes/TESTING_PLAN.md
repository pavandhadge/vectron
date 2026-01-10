# Vectron: Comprehensive Testing Plan

## 1. Introduction

This document outlines a comprehensive testing plan for the Vectron distributed vector database. The goal of this plan is to ensure the correctness, reliability, performance, and resilience of the entire system.

The testing strategy is divided into multiple levels, each focusing on a different aspect of the system.

## 2. Levels of Testing

### 2.1. Unit Testing

**Goal:** To verify that individual components (functions, structs, etc.) work correctly in isolation.

**Scope:**
- **`placementdriver`:**
    - FSM logic (`fsm.go`): Test the state transitions of the finite state machine (e.g., creating collections, registering workers, assigning shards).
    - Hashing logic in `server.go`: Test that the consistent hashing correctly maps vector IDs to shards.
- **`worker`:**
    - Storage engine (`storage.go`): Test the CRUD operations, vector operations, and snapshot/restore functionality of the `PebbleDB` storage.
    - Shard manager (`manager.go`): Test the logic for identifying which shards to start and stop.
    - State machine (`state_machine.go`): Test the `Update` and `Lookup` methods of the per-shard state machine.
- **`apigateway`:**
    - Translator functions (`translator.go`): Test the conversion between public API objects and internal gRPC objects.
    - Middleware (`middleware/*.go`): Test the authentication, rate limiting, and logging middleware.

### 2.2. Integration Testing

**Goal:** To verify that different components and services work together as expected.

**Scope:**
- **`placementdriver` + `worker` integration:**
    - Test that a worker can successfully register with the `placementdriver`.
    - Test that a worker receives correct shard assignments via heartbeats.
    - Test that the `ShardManager` in the worker correctly starts and stops Raft clusters based on the assignments.
- **`apigateway` + `placementdriver` integration:**
    - Test that the `apigateway` can successfully connect to the `placementdriver`.
    - Test that `CreateCollection` requests are correctly forwarded and executed.
    - Test that `GetWorker` requests are correctly handled and return the right worker address and shard ID.
- **`apigateway` + `worker` integration:**
    - Test that the `apigateway` can successfully connect to a worker.
    - Test that `Upsert`, `Search`, `Get`, and `Delete` requests are correctly forwarded to the correct shard on the worker.

### 2.3. End-to-End (E2E) Testing

**Goal:** To test the entire system from the client's perspective, simulating real-world usage.

**Scope:**
- Use the client libraries (Python, Go, JS) to interact with the system.
- **Scenario 1: Basic lifecycle**
    1. Start a 3-node `placementdriver` cluster.
    2. Start 3 `worker` nodes.
    3. Start the `apigateway`.
    4. Use a client to create a collection.
    5. Use a client to upsert a large number of vectors.
    6. Use a client to search for vectors and verify the results.
    7. Use a client to get and delete individual vectors.
- **Scenario 2: Shard balancing (Future)**
    - Test that if a shard grows too large, the `placementdriver` splits it and rebalances the replicas.
- **Scenario 3: Node failure (see Chaos Testing)**

### 2.4. Performance Testing

**Goal:** To measure the system's performance (latency, throughput) under various loads.

**Scope:**
- **Upsert throughput:** Measure how many vectors per second can be ingested.
- **Search latency:** Measure the time it takes to perform a search query, especially at different `k` values.
- **Scalability:** Measure how the performance changes as more `worker` nodes are added to the cluster.

### 2.5. Chaos Testing

**Goal:** To test the system's resilience to failures.

**Scope:**
- **Worker node failure:**
    - Kill a `worker` node process.
    - Verify that the `placementdriver` detects the failure and re-assigns the shards that were on the failed worker.
    - Verify that the system remains available for reads and writes (with a brief interruption for leader re-election).
- **`placementdriver` node failure:**
    - Kill a non-leader `placementdriver` node. Verify the cluster remains operational.
    - Kill the leader `placementdriver` node. Verify that a new leader is elected and the cluster continues to operate.
- **Network partitioning:**
    - Use network tools (e.g., `iptables`) to simulate a network partition between nodes.
    - Verify that the system remains consistent and recovers when the partition is healed.

## 3. Testing Environments

- **Local Environment:** Use Docker Compose to set up a multi-node cluster on a single machine for development and integration testing.
- **Staging Environment:** A dedicated, production-like environment in the cloud (e.g., on Kubernetes) for E2E, performance, and chaos testing.

## 4. Tooling

- **Unit Testing:** Go's built-in `testing` package.
- **Integration/E2E Testing:** A test runner written in Go or Python that uses the client libraries to drive the tests.
- **Performance Testing:** A tool like `k6`, `JMeter`, or a custom Go application to generate load.
- **Chaos Testing:** A tool like `Chaos Mesh` or custom scripts to inject failures.
- **CI/CD:** A CI/CD pipeline (e.g., GitHub Actions) to automate the execution of the tests on every code change.
