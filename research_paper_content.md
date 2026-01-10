# A Comparative Analysis of Architectural Models for Distributed Vector Databases

## Abstract

The proliferation of large-scale machine learning and generative AI has created a critical need for databases capable of storing and searching high-dimensional vector embeddings at scale. While many vector search algorithms exist, the principles for designing a scalable, resilient, and manageable *distributed* vector database are not well-established. Different open-source systems have adopted vastly different architectural patterns—from disaggregated microservices to tightly-coupled replication groups—each with implicit trade-offs. This paper presents a rigorous comparative analysis of three architectural archetypes for distributed vector databases. We introduce **Vectron**, a novel system designed with a decoupled control plane and a "multi-Raft" data plane, as a case study. We compare its design against two leading open-source systems: **Milvus**, representing a disaggregated, shared-storage microservices architecture, and **Weaviate**, representing a modular, clustered architecture. We analyze each system's design with respect to its data and control plane, replication and consistency model, and operational dependencies. By dissecting these distinct approaches, we provide a framework for architects to reason about the fundamental trade-offs in performance, scalability, consistency, and operational complexity when designing and deploying large-scale vector search systems.

---

## 1. Introduction

The rapid adoption of deep learning has transformed how information is represented and processed. Models now routinely convert complex data—such as text, images, and audio—into high-dimensional numerical vectors, or "embeddings." The proximity of these vectors in a geometric space corresponds to their semantic similarity. This paradigm shift has fueled the demand for a new class of databases, known as vector databases, designed specifically to index and search these embeddings for Approximate Nearest Neighbor (ANN) retrieval.

While single-node vector search libraries like Faiss are mature, implementing a distributed vector database that is scalable, highly available, and easy to manage presents a significant system design challenge. A distributed vector database must not only perform efficient ANN search but also handle data sharding, replication, consistency, fault tolerance, and cluster management. There is currently no consensus on the optimal architecture for such a system.

Existing systems present a spectrum of design choices. Some, like Milvus, adopt a highly disaggregated microservices architecture, separating functions like querying, indexing, and data management into distinct, independently scalable services that rely on external dependencies like message queues and object storage. Others, like Weaviate, favor a more integrated, modular design where nodes are homogeneous and dependencies are minimized.

This paper explores a third approach through the design of **Vectron**, a distributed vector database featuring a clean separation between its control and data planes. The control plane is managed by a dedicated, Raft-based consensus group, while the data plane consists of workers that manage data shards, with each shard being its own independent Raft group. This "multi-Raft" design emphasizes strong consistency and aims for operational simplicity by minimizing distinct service types.

The central contribution of this paper is a qualitative and architectural comparison of these three archetypes—Vectron, Milvus, and Weaviate. We dissect the core design of each system, analyzing their approaches to:
1.  **State Management and Consensus:** How cluster metadata and data are replicated and kept consistent.
2.  **Data and Control Plane Coupling:** The degree of separation between cluster management and data operations.
3.  **Operational Complexity:** The number of components and dependencies required for a deployment.

By evaluating the trade-offs inherent in each design, this paper provides a foundational understanding for practitioners and researchers building the next generation of large-scale vector search engines.

---

## 2. Background

### 2.1 Vector Embeddings and ANN Search

Vector embeddings are dense numerical representations of data. The process of finding the most similar vectors to a given query vector is framed as a Nearest Neighbor (NN) search. However, performing an exact NN search in high-dimensional space is computationally prohibitive. Consequently, vector databases rely on Approximate Nearest Neighbor (ANN) algorithms, which trade perfect accuracy for a dramatic improvement in search speed.

**Hierarchical Navigable Small World (HNSW)** is a state-of-the-art ANN algorithm that has become a de facto standard. HNSW builds a multi-layered graph where links are established between vectors. Searches traverse this graph from a high-level entry point, progressively moving to denser layers to quickly navigate to the region of the query vector and find its nearest neighbors.

### 2.2 Distributed Consensus and Raft

In a distributed system, ensuring that all nodes agree on the system's state, even in the presence of failures, is a fundamental problem solved by consensus algorithms. **Raft** is a consensus algorithm designed to be more understandable than its predecessor, Paxos. It works by electing a "leader" for a group of nodes (a cluster). All changes to the system's state are sent to the leader as log entries. The leader replicates these entries to the other nodes (followers). An entry is only "committed" and applied to the state machine once it has been successfully stored on a quorum (a majority) of nodes. This ensures that the state machine remains consistent across all nodes.

---

## 3. System Architecture: A Case Study of Vectron

Vectron is designed around a clear architectural principle: a strict separation between a centralized control plane and a distributed data plane.

### 3.1 The Control Plane: Placement Driver (PD)

The "brain" of the Vectron cluster is the **Placement Driver (PD)**. The PD is itself a distributed service, forming a small, independent Raft cluster (typically 3 or 5 nodes) to ensure its own fault tolerance. It is solely responsible for managing the global metadata of the entire Vectron cluster. This metadata, maintained in a Replicated State Machine (RSM), includes:
*   A list of all worker nodes in the cluster, their network addresses, and their health status (via heartbeats).
*   The schema of all created collections (name, vector dimension, etc.).
*   The mapping of collections to a set of shards.
*   The assignment of shard replicas to specific worker nodes.

By centralizing metadata management, the PD provides a single source of truth for the cluster's topology.

### 3.2 The Data Plane: Workers and the Multi-Raft Model

The "brawn" of the cluster is the **worker**. Workers are homogeneous nodes responsible for storing data, building indexes, and executing searches. The data plane is built on a "multi-Raft" model:

*   **Shards as Raft Groups:** Each data shard is an independent Raft consensus group. A shard is not just a static partition of data; it is a living, replicating state machine. A shard with a replication factor of 3, for instance, consists of three replicas distributed across three different worker nodes.
*   **Multi-Raft on Workers:** A single worker node can host replicas for many different shards. It accomplishes this by running a single `dragonboat` NodeHost process, which multiplexes and manages all the Raft groups the worker is a member of.

This design is illustrated below:

```
+-----------------------------------+
|      Placement Driver Cluster     | (Single Raft Group for Metadata)
|    [PD-1]   [PD-2]   [PD-3] (L)   |
+-----------------------------------+
      |               ^
      | Heartbeat &   | Shard Assignments
      | Assignments   | & Health Status
      v               |
+-------------------------------------------------------------+
|                             Workers                           |
| +------------------+ +------------------+ +------------------+ |
| |     Worker 1     | |     Worker 2     | |     Worker 3     | |
| | [NodeHost]       | | [NodeHost]       | | [NodeHost]       | |
| | +--------------+ | | +--------------+ | | +--------------+ | |
| | | Shard A (L)  | | | | Shard A (F)  | | | | Shard B (L)  | | |
| | +--------------+ | | +--------------+ | | +--------------+ | |
| | +--------------+ | | +--------------+ | | +--------------+ | |
| | | Shard B (F)  | | | | Shard C (F)  | | | | Shard C (L)  | | |
| | +--------------+ | | +--------------+ | | +--------------+ | |
| +------------------+ +------------------+ +------------------+ |
+-------------------------------------------------------------+

(L) = Leader, (F) = Follower
Raft traffic for Shard A: Worker 1 <--> Worker 2
Raft traffic for Shard B: Worker 1 <--> Worker 3
Raft traffic for Shard C: Worker 2 <--> Worker 3
```

### 3.3 The Write Path

1.  A client sends a `StoreVector` request to a stateless **API Gateway**.
2.  The gateway asks the **Placement Driver** which shard is responsible for the vector's ID (determined via consistent hashing). The PD replies with the address of a worker hosting a replica of that shard (ideally the leader).
3.  The gateway forwards the `StoreVector` request to the target worker.
4.  The worker's gRPC server receives the request, identifies the target shard ID, and proposes the `StoreVector` command to that shard's Raft log using `SyncPropose`.
5.  The Raft protocol replicates the command to the other worker(s) hosting replicas of the shard.
6.  Once the command is committed by a quorum, it is applied to the state machine on all replicas, where the vector is written to a local key-value store (PebbleDB) and added to the in-memory HNSW index.

### 3.4 The Read Path (Search)

1.  A client sends a `Search` request to the **API Gateway**.
2.  The gateway asks the **Placement Driver** for a relevant worker for the collection (since a search may span a full shard).
3.  The gateway forwards the `Search` request to the target worker.
4.  The worker's gRPC server receives the request and performs a `SyncRead` on the target shard's Raft group. `SyncRead` is a linearizable read operation that ensures the query is executed on the shard leader against the latest committed state.
5.  On the leader's state machine, the query is executed against the in-memory HNSW index.
6.  The results are returned up the chain to the client.

### 3.5 Operational Characteristics

*   **Dependencies:** The system is self-contained. Besides the Go runtime, it has no external dependencies like Zookeeper, message queues, or object stores.
*   **Simplicity:** There are only two distinct service types to deploy: `placementdriver` and `worker`. The `apigateway` is stateless and can be scaled horizontally as needed.
*   **Consistency:** The dual-Raft design provides strong consistency guarantees for both metadata and data operations. All reads can be made linearizable, and all writes are durably stored via consensus.

---

## 4. Methodology for Comparative Analysis

To evaluate the architectural trade-offs, we propose a series of experiments to be conducted on a standardized Kubernetes cluster. Each system (Vectron, Milvus, Weaviate) will be deployed according to its production-recommended guidelines.

*   **Testbed:** A Kubernetes cluster with uniform node specifications (e.g., 8 vCPU, 32 GB RAM per node).
*   **Dataset:** The SIFT-1M dataset (1 million 128-dimensional vectors) will be used for performance benchmarks.

The following experiments will be conducted:

1.  **Ingestion Performance:** We will measure the time and average throughput (vectors/sec) required to ingest the entire 1M vector dataset. This will be repeated with varying client concurrency levels (1, 8, 32 clients) and batch sizes (100, 1000, 5000 vectors).

2.  **Search Performance and Accuracy:** After ingestion, we will execute a workload of 10,000 queries from the SIFT query set. We will measure the p99 search latency and the recall@10 for each system at different client loads (100 QPS, 500 QPS, 1000 QPS).

3.  **Fault Tolerance:**
    *   **Control Plane Failure:** During a mixed read/write workload, the leader of the control plane (`placementdriver` for Vectron, `etcd` for Milvus) will be terminated. We will measure the time until the system is able to serve write requests again.
    *   **Data Node Failure:** During the same workload, a single data node will be terminated. We will measure the time until shards hosted on that node have successfully failed over and can once again serve reads and writes.

4.  **Operational Complexity Analysis:** We will conduct a qualitative analysis, documenting the step-by-step process of deploying each system, the number of configuration files and parameters touched, and the number of distinct pods/services running in the Kubernetes cluster for a minimal high-availability setup.
