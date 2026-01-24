# Reranker Service TODO

## High Priority (MVP)

### Rule-Based Strategy
- [ ] Implement `RuleBasedStrategy` in `internal/strategies/rule/`
  - [ ] Keyword matching (exact + fuzzy)
  - [ ] TF-IDF scoring using Go libraries
  - [ ] Metadata boosting (configurable weights)
  - [ ] Configurable rules from YAML/JSON
  - [ ] Unit tests with diverse queries

### Caching
- [ ] Redis cache implementation
  - [ ] Connection pooling
  - [ ] TTL management
  - [ ] Pattern-based invalidation
  - [ ] Serialization optimization
- [ ] Improve MemoryCache with LRU eviction
- [ ] Cache metrics (hit rate, size, evictions)

### Integration
- [ ] Generate gRPC code from proto
- [ ] Add to main Makefile/build scripts
- [ ] Integration tests with Worker service
- [ ] Docker container setup
- [ ] Kubernetes deployment manifests

### Monitoring
- [ ] Prometheus metrics exporter
  - [ ] Latency histograms per strategy
  - [ ] Cache metrics
  - [ ] Error counters
- [ ] Structured logging (JSON format)
- [ ] Distributed tracing (Jaeger/OpenTelemetry)

---

## Medium Priority

### LLM-Based Strategy
- [ ] Implement `LLMStrategy` in `internal/strategies/llm/`
  - [ ] OpenAI client integration
  - [ ] Grok API support
  - [ ] Prompt engineering for reranking
  - [ ] Batch processing for efficiency
  - [ ] Rate limiting and retries
  - [ ] Cost tracking
- [ ] Fine-tuning pipeline with LoRA
  - [ ] Feedback data format
  - [ ] Training scripts
  - [ ] Model versioning

### Feedback Loop
- [ ] Feedback API endpoint (or use API Gateway)
- [ ] Feedback storage schema
- [ ] Batch processing job for rule updates
- [ ] Metrics on feedback impact

### Advanced Features
- [ ] A/B testing framework
  - [ ] Strategy routing based on user segments
  - [ ] Experiment tracking
  - [ ] Statistical significance testing
- [ ] Hybrid strategies (rule + LLM fallback)
- [ ] Dynamic strategy selection based on query type

---

## Low Priority (Future)

### RL-Based Strategy
- [ ] Implement `RLStrategy` in `internal/strategies/rl/`
  - [ ] ONNX Runtime integration
  - [ ] State representation (query + doc embeddings)
  - [ ] Reward function from feedback
  - [ ] Training pipeline (PPO/DQN)
  - [ ] Model serving infrastructure
  - [ ] Online learning capability

### Optimization
- [ ] Multi-stage reranking (cheap first, expensive if needed)
- [ ] GPU acceleration for LLM/RL strategies
- [ ] Connection pooling for gRPC clients
- [ ] Response streaming for large result sets

### Operations
- [ ] Health check endpoint
- [ ] Graceful strategy hot-swapping
- [ ] Configuration reload without restart
- [ ] Backup/restore for cache state
- [ ] Multi-region deployment

---

## Documentation
- [ ] Strategy development guide
- [ ] API examples for each language (Go, Python, JS)
- [ ] Performance tuning guide
- [ ] Troubleshooting common issues
- [ ] Research paper section on reranking approach

---

## Research & Experiments
- [ ] Benchmark against baseline (no reranking)
- [ ] Compare strategies on standard datasets (BEIR, MS MARCO)
- [ ] Measure impact of feedback loop
- [ ] Cost-benefit analysis for LLM vs RL
- [ ] Explore GNN-based reranking

---

## Notes
- Start with rule-based for quick wins
- LLM implementation can reuse OpenAI Go SDK
- RL requires separate Python service initially (can use gRPC bridge)
- Consider using existing reranking models (e.g., cross-encoders from Hugging Face)
