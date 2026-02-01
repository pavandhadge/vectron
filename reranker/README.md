# Vectron Reranker Service

The Reranker Service is a modular microservice that enhances vector search results by applying intelligent reranking strategies beyond simple distance-based metrics. It provides a unified gRPC interface with pluggable implementations for rule-based, LLM-based, and RL-based reranking.

## Architecture Overview

```
┌─────────────────┐
│  API Gateway    │
└────────┬────────┘
         │ query
         ▼
┌─────────────────┐
│ Retrieval Svc   │──► Vector DB (HNSW)
│  (Worker)       │    returns top-K
└────────┬────────┘
         │ candidates
         ▼
┌─────────────────┐
│   Reranker      │──► Redis Cache
│   Service       │    (optional)
└────────┬────────┘
         │ reranked
         ▼
┌─────────────────┐
│  API Gateway    │──► Client
└─────────────────┘
```

## Features

- **Pluggable Strategies**: Easily swap between rule-based, LLM, or RL implementations
- **Unified gRPC Interface**: Consistent API regardless of underlying strategy
- **Caching Support**: Redis or in-memory caching to reduce latency and costs
- **Performance Monitoring**: Built-in metrics for latency, cache hits, and errors
- **Hot Swapping**: Change strategies without downtime (for A/B testing)
- **Feedback Integration**: Cache invalidation API for incorporating user feedback

## gRPC Interface

### Proto Definition

```protobuf
service RerankService {
  rpc Rerank(RerankRequest) returns (RerankResponse);
  rpc GetStrategy(GetStrategyRequest) returns (GetStrategyResponse);
  rpc InvalidateCache(InvalidateCacheRequest) returns (InvalidateCacheResponse);
}
```

See [`../shared/proto/reranker/reranker.proto`](../shared/proto/reranker/reranker.proto) for the complete definition.

## Strategy Interface

All reranking implementations must satisfy the `Strategy` interface:

```go
type Strategy interface {
    Name() string
    Version() string
    Rerank(ctx context.Context, req *RerankInput) (*RerankOutput, error)
    Config() map[string]string
}
```

### Planned Implementations

1. **Rule-Based** (`internal/strategies/rule/`)
   - Keyword matching with TF-IDF
   - Metadata boosting/penalties
   - Configurable heuristics
   - Fast (<10ms), deterministic

2. **LLM-Based** (`internal/strategies/llm/`)
   - OpenAI/Grok API integration
   - Cross-encoder scoring
   - Fine-tunable via LoRA
   - Higher accuracy but slower (200-500ms)

3. **RL-Based** (`internal/strategies/rl/`)
   - Policy-based scoring
   - Continuous learning from feedback
   - ONNX model serving
   - Adaptive over time

## Configuration

### Environment Variables

```bash
# Server settings
RERANKER_PORT=50051

# Strategy selection
RERANKER_STRATEGY=rule  # Options: rule, llm, rl

# Cache settings
CACHE_BACKEND=memory    # Options: memory, redis
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
CACHE_TTL=24h

# Strategy-specific configs (examples)
RULE_BOOST_RECENT=0.2
LLM_API_KEY=sk-...
LLM_MODEL=gpt-4
RL_MODEL_PATH=/models/reranker.onnx
```

### Configuration File (Future)

```yaml
reranker:
  port: 50051
  strategy:
    type: rule
    config:
      boost_keywords: 0.3
      penalty_errors: -0.5
  cache:
    backend: redis
    ttl: 24h
    redis:
      addr: localhost:6379
```

## Building and Running

### Prerequisites

- Go 1.24+
- Protocol Buffers compiler (`protoc`)
- gRPC Go plugin

### Generate gRPC Code

```bash
cd shared/proto/reranker
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       reranker.proto
```

### Build

```bash
cd reranker
go build -o bin/reranker ./cmd/reranker
```

### Run

```bash
# With default settings (stub strategy)
./bin/reranker

# With custom configuration
RERANKER_STRATEGY=rule RERANKER_PORT=50051 ./bin/reranker
```

### Docker (Future)

```bash
docker build -t vectron-reranker .
docker run -p 50051:50051 -e RERANKER_STRATEGY=rule vectron-reranker
```

## Testing

```bash
# Unit tests
go test ./internal/...

# Integration tests with grpcurl
grpcurl -plaintext -d '{
  "query": "machine learning tutorial",
  "candidates": [
    {"id": "doc1", "score": 0.85, "metadata": {"title": "ML Basics"}},
    {"id": "doc2", "score": 0.80, "metadata": {"title": "Deep Learning"}}
  ],
  "top_n": 2
}' localhost:50051 reranker.RerankService/Rerank
```

## Integration with Vectron

### From API Gateway

```go
// Call Worker for initial retrieval
candidates := workerClient.Search(ctx, query, topK=100)

// Call Reranker for enhancement
reranked := rerankerClient.Rerank(ctx, &reranker.RerankRequest{
    Query: query,
    Candidates: candidates,
    TopN: 10,
})

// Return reranked results to client
return reranked.Results
```

### Feedback Loop

```go
// On negative feedback
rerankerClient.InvalidateCache(ctx, &reranker.InvalidateCacheRequest{
    Query: query,
    CandidateIds: []string{badResultID},
})

// Batch update (daily job)
updateRulesFromFeedback()  // Adjusts boost/penalty weights
```

## Performance Targets

| Strategy   | Latency (p99) | Throughput | Cost/1K queries |
|------------|---------------|------------|-----------------|
| Rule-based | <10ms         | ~10K QPS   | $0             |
| LLM-based  | <500ms        | ~100 QPS   | $5-10          |
| RL-based   | <50ms         | ~1K QPS    | $0.50          |

## Monitoring

Key metrics to track:
- Reranking latency per strategy
- Cache hit rate
- Error rate by strategy
- Score distribution changes
- Feedback incorporation lag

## Roadmap

- [x] gRPC interface definition
- [x] Pluggable strategy pattern
- [x] Basic cache interface
- [ ] Rule-based implementation
- [ ] Redis cache backend
- [ ] LLM strategy (OpenAI integration)
- [ ] RL strategy (ONNX serving)
- [ ] Prometheus metrics exporter
- [ ] Distributed tracing (OpenTelemetry)
- [ ] A/B testing framework
- [ ] Feedback pipeline integration

## Related Documentation

- [Vectron Architecture](../docs/Vectron_Architecture.md)
- [Worker Service](../docs/Worker_Service.md)
- [API Gateway](../docs/APIGateway_Service.md)
- [System Integration](../docs/System_Integration.md)

## Contributing

Follow the same patterns as other Vectron services:
- Keep strategies in `internal/strategies/<type>/`
- Add tests for new strategies
- Update proto definitions cautiously (maintain backward compatibility)
- Document strategy-specific configuration

## License

See main Vectron repository.
