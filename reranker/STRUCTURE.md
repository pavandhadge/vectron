# Reranker Service Structure

```
reranker/
├── cmd/
│   └── reranker/
│       └── main.go              # Entry point, server initialization
├── internal/
│   ├── grpc.go                  # gRPC server implementation
│   ├── strategy.go              # Strategy interface definitions
│   └── strategies/              # Strategy implementations
│       ├── rule/                # Rule-based reranking
│       ├── llm/                 # LLM-based reranking (to be added)
│       └── rl/                  # RL-based reranking (to be added)
├── go.mod                       # Go module definition
├── README.md                    # Service documentation
└── TODO.md                      # Implementation roadmap

# Proto files are located in:
../shared/proto/reranker/
    └── reranker.proto           # gRPC service definition
```

## Quick Start

1. **Generate gRPC code:**
   ```bash
   cd shared/proto/reranker
   protoc --go_out=. --go_opt=paths=source_relative \
          --go-grpc_out=. --go-grpc_opt=paths=source_relative \
          reranker.proto
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
