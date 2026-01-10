# Research Paper Ideas: A Comparative Study of Distributed Vector Database Architectures

This document outlines potential research questions, a framework for a comparative analysis, and key evaluation metrics for a research paper centered on the Vectron database.

---

### **Title Suggestions**

*   "A Comparative Analysis of Architectural Models for Distributed Vector Databases"
*   "Vectron: A Case Study in Decoupled Control Planes and Multi-Raft Consensus for Vector Search"
*   "Scalability, Consistency, and Complexity: Trade-offs in Distributed Vector Database Design"
*   "Performance and Fault Tolerance of Microservices vs. Multi-Raft Architectures for Approximate Nearest Neighbor Search"

---

### **Abstract & Problem Statement**

**Problem:** The proliferation of large-scale machine learning has created a critical need for databases capable of storing and searching high-dimensional vector embeddings. While many vector search algorithms exist, the principles for designing a scalable, resilient, and manageable *distributed* vector database are not well-established. Different open-source systems have adopted vastly different architectural patterns, from disaggregated microservices to tightly-coupled replication groups, each with implicit trade-offs.

**Contribution:** This paper presents a rigorous comparative analysis of three architectural archetypes for distributed vector databases. We introduce **Vectron**, a novel system designed with a decoupled control plane and a "multi-Raft" data plane, and compare its performance, scalability, and operational characteristics against two leading open-source systems: **Milvus**, representing a disaggregated microservices architecture, and **Weaviate**, representing a clustered, modular architecture. Through a series of targeted experiments, we will quantify the trade-offs between these models, providing a framework for architects to reason about the design of future large-scale vector search systems.

---

### **Core Thesis & Research Questions**

**Thesis:** The architectural design of a distributed vector database has a fundamental impact on its operational complexity, fault tolerance profile, and the balance it strikes between ingestion performance and search consistency. A decoupled control plane, as implemented in Vectron, can simplify scalability and cluster management, while a multi-Raft data plane offers strong consistency at the cost of potentially higher write latency.

**Primary Research Question:**
*   How does a decoupled, multi-Raft architecture (Vectron) compare to microservices-based (Milvus) and modular clustered (Weaviate) architectures across the key dimensions of performance, scalability, fault tolerance, and operational complexity?

**Secondary Research Questions:**
1.  What is the performance overhead (latency and throughput) of using two tiers of Raft consensus (a metadata layer and a per-shard data layer) for vector ingestion?
2.  How does the choice of consistency model (e.g., Vectron's linearizable reads vs. Milvus's tunable consistency) impact search accuracy and latency under load?
3.  What is the impact of control plane and data plane separation on the system's ability to recover from different node failure scenarios (e.g., loss of a `placementdriver` vs. loss of a Milvus query node)?
4.  Can we quantify the "operational complexity" of each system in terms of the number of distinct service types, external dependencies, and configuration parameters required for a production deployment?

---

### **Framework for Comparative Study**

#### **1. Systems Under Test**

*   **Vectron (Decoupled, Multi-Raft):**
    *   **Control Plane:** A dedicated, Raft-based `placementdriver` cluster that manages all cluster metadata, shard placement, and service discovery.
    *   **Data Plane:** A set of homogeneous `worker` nodes. Each data shard is its own independent Raft cluster, with replicas distributed across multiple workers.
    *   **Key Feature:** Strong separation of concerns. The PD is the "brain"; workers are "brawn." Stateless API gateways route requests.

*   **Milvus (Disaggregated Microservices):**
    *   **Architecture:** Composed of multiple, distinct microservices for different functions: Query Nodes, Index Nodes, Data Nodes, and a Proxy.
    *   **Dependencies:** Relies heavily on external services for coordination and storage: `etcd` for metadata, a message queue (e.g., Pulsar, Kafka) for data streaming, and an object store (e.g., S3, MinIO) for log storage.
    *   **Key Feature:** High degree of disaggregation allows for independent scaling of different functions (e.g., add more Query Nodes to handle more searches).

*   **Weaviate (Modular, Clustered):**
    *   **Architecture:** Can run as a single node or a distributed cluster. In a cluster, nodes are homogeneous.
    *   **Dependencies:** Uses an internal Raft implementation (based on HashiCorp's Raft) to replicate schema and metadata across nodes. Data is sharded and replicated, but the replication mechanism can be configured.
    *   **Key Feature:** Aims for operational simplicity with fewer external dependencies while still providing distributed capabilities.

#### **2. Evaluation Metrics & Experiments**

*   **A. Performance Analysis**
    *   **Dataset:** Use standard high-dimensional vector datasets (e.g., SIFT-1M, GIST-1M, ANN-Benchmarks).
    *   **Ingestion Throughput:** Measure the number of vectors per second that can be inserted into the database as a function of batch size and client concurrency. This tests the efficiency of the write path.
    *   **Search Latency & Recall:** For a fixed index, measure the p99 latency of search requests at varying levels of QPS (Queries Per Second). Simultaneously, measure the recall@K (e.g., recall@10) to quantify search accuracy. This tests the read path and index efficiency.
    *   **Indexing Time:** Measure the total time required to build the HNSW index after inserting a large number of vectors.

*   **B. Scalability Analysis**
    *   **Read Scalability:** With a fixed data size, measure how search latency and throughput are affected as the number of query-serving nodes is increased (e.g., adding more `apigateway` nodes in Vectron, more Query Nodes in Milvus, more nodes in Weaviate).
    *   **Write Scalability:** Measure how ingestion throughput changes as the number of data-handling nodes is increased (`worker` nodes in Vectron, Data Nodes in Milvus).
    *   **Data Scalability:** Measure how search latency degrades as the total number of vectors in the database increases from 1M to 10M to 100M.

*   **C. Fault Tolerance Analysis**
    *   **Control Plane Failure:**
        *   **Vectron:** Kill the `placementdriver` leader. Measure the time until a new leader is elected and the API becomes fully responsive again.
        *   **Milvus:** Kill the `etcd` leader. Measure the time to recovery.
    *   **Data Node Failure:**
        *   **Vectron:** Kill a `worker` node hosting several shard leaders. Measure the time until the remaining shard replicas elect new leaders and become writable/readable again.
        *   **Milvus/Weaviate:** Kill a node holding data segments/shards. Measure the time until the system recovers and can serve queries for the affected data.
    *   **Stateless Node Failure:** Kill a stateless component (`apigateway` in Vectron, Proxy in Milvus). Measure the impact on in-flight requests and the system's ability to continue serving traffic.

*   **D. Operational Complexity (Qualitative & Quantitative)**
    *   **Component Count:** Tally the number of distinct binary/service types that must be deployed for a production-grade setup.
    *   **Dependency Count:** Tally the number of required external dependencies (e.g., etcd, Pulsar, S3).
    *   **Configuration Surface Area:** Compare the number of key configuration parameters required to set up a distributed cluster for each system.
    *   **Deployment Story:** Briefly describe the process of deploying each system onto a Kubernetes cluster.
