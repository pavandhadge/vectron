# Reranker Service Structure

```
reranker/
├── cmd/
│   └── reranker/
│       └── main.go              # Entry point, server initialization
├── internal/
│   ├── grpc.go                  # gRPC server implementation
│   ├── strategy.go              # Strategy interface definitions
│   └── strategies/              # Strategy implementations (to be added)
│       ├── rule/                # Rule-based reranking
│       ├── llm/                 # LLM-based reranking
│       └── rl/                  # RL-based reranking
├── proto/
│   └── reranker/
│       └── reranker.proto       # gRPC service definition
├── go.mod                       # Go module definition
├── README.md                    # Service documentation
└── TODO.md                      # Implementation roadmap
```

## Quick Start

1. **Generate gRPC code:**
   ```bash
   cd reranker
   protoc --go_out=. --go_opt=paths=source_relative \
          --go-grpc_out=. --go-grpc_opt=paths=source_relative \
          proto/reranker/reranker.proto
   ```

2. **Build:**
   ```bash
   go build -o bin/reranker ./cmd/reranker
   ```

3. **Run:**
   ```bash
   ./bin/reranker --strategy=rule --port=50051
   ```

## Next Steps

See [TODO.md](TODO.md) for implementation priorities.
