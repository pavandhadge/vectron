# Academic & Technical References

This document provides a list of key academic papers and technical resources relevant to the design of the Vectron system and the comparative analysis proposed in the research paper.

---

### **Core Concepts: Consensus and Indexing**

1.  **Raft Consensus Algorithm**
    *   **Paper:** Ongaro, D., & Ousterhout, J. (2014). *In Search of an Understandable Consensus Algorithm*. In 2014 USENIX Annual Technical Conference (USENIX ATC 14) (pp. 305-319).
    *   **Relevance:** This is the foundational paper describing the Raft algorithm. It is essential for understanding the consensus mechanism used by both the `placementdriver` and the per-shard replication groups in Vectron. The paper's emphasis on understandability is key to its widespread adoption.

2.  **Hierarchical Navigable Small World (HNSW) Graphs for ANN Search**
    *   **Paper:** Malkov, Y. A., & Yashunin, D. A. (2018). *Efficient and robust approximate nearest neighbor search using Hierarchical Navigable Small World graphs*. IEEE transactions on pattern analysis and machine intelligence, 42(4), 824-836.
    *   **Relevance:** This paper introduces the HNSW algorithm, which is the state-of-the-art for high-performance approximate nearest neighbor search and is used in Vectron's `worker` nodes. Understanding this paper is critical to reasoning about search performance.

3.  **The CAP Theorem**
    *   **Paper:** Gilbert, S., & Lynch, N. (2002). *Brewer's conjecture and the feasibility of consistent, available, partition-tolerant web services*. ACM SIGACT News, 33(2), 51-59.
    *   **Relevance:** This foundational paper in distributed systems formalizes the trade-offs between Consistency, Availability, and Partition Tolerance. Vectron's design, which heavily favors consistency (via Raft), can be analyzed through the lens of the CAP theorem.

### **Key-Value Stores and Database Engines**

4.  **The Log-Structured Merge-Tree (LSM-Tree)**
    *   **Paper:** O'Neil, P., Cheng, E., Gawlick, D., & O'Neil, E. (1996). *The log-structured merge-tree (LSM-tree)*. Acta Informatica, 33(4), 351-385.
    *   **Relevance:** PebbleDB, used by Vectron's `worker` nodes, is an LSM-Tree based key-value store. This paper describes the fundamental data structure that provides high write throughput, which is critical for vector ingestion performance.

5.  **Dragonboat: A Multi-Group Raft Library**
    *   **Resource:** `github.com/lni/dragonboat`
    *   **Relevance:** This is the specific library used by Vectron to implement its "multi-Raft" architecture. The library's documentation and design philosophy are directly relevant to how a single `worker` node can efficiently manage hundreds or thousands of independent Raft groups (shards).

### **Comparable Vector Database Systems**

6.  **Milvus: A Purpose-Built Vector Data Management System**
    *   **Paper:** Wang, J., et al. (2021). *Milvus: A Purpose-Built Vector Data Management System*. In Proceedings of the 2021 International Conference on Management of Data (SIGMOD '21). Association for Computing Machinery, New York, NY, USA, 2633–2647.
    *   **Relevance:** This paper provides a detailed architectural overview of Milvus, the primary system for comparison. It details its microservices-based architecture, reliance on a log broker (Pulsar/Kafka), and separation of concerns, providing a direct contrast to Vectron's design.

7.  **Weaviate: A Cloud-Native Vector Database**
    *   **Resource:** Weaviate documentation and blog posts on its architecture (`weaviate.io/blog/architecture`).
    *   **Relevance:** While a formal paper may be less central, the official documentation and technical blogs describe Weaviate's architecture, including its modular design, sharding strategy, and internal use of Raft for schema consensus. This is the second major point of comparison.

### **General Distributed Systems and Database Design**

8.  **Designing Data-Intensive Applications**
    *   **Book:** Kleppmann, M. (2017). *Designing Data-Intensive Applications: The Big Ideas Behind Reliable, Scalable, and Maintainable Systems*. O'Reilly Media.
    *   **Relevance:** This book is an essential text for understanding the broader context of distributed systems. It provides in-depth, accessible explanations of replication, partitioning (sharding), transactions, and consistency models that are directly applicable to the analysis of Vectron, Milvus, and Weaviate.
