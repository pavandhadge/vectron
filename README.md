# Vectron

A distributed vector database designed for high-performance similarity search. Built with Go microservices using Raft consensus, providing client libraries for Go, Python, and JavaScript.

[![Go Version](https://img.shields.io/badge/Go-1.24-blue)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

## 🚀 Quick Start

```bash
# Build all services
make build

# Generate protobuf code
bash generate-all.sh

# Configure env files (per-service)
# Place .env.<service> or <service>.env in repo root, or use env/<service>.env

# Run services (see docs for full setup)
./bin/placementdriver --node-id=1 --grpc-addr=localhost:6001 --raft-addr=localhost:7001
./bin/worker --node-id=1 --grpc-addr=localhost:9090 --pd-addrs=localhost:6001
./bin/apigateway
./bin/authsvc
./bin/reranker  # Optional: Intelligent result reranking
```

## 📋 What's Inside

| Component | Purpose | Tech |
|-----------|---------|------|
| **API Gateway** | Public API entry point with auth, feedback & routing | gRPC + HTTP/REST + SQLite |
| **Placement Driver** | Cluster coordinator with Raft consensus | Dragonboat |
| **Worker** | Data nodes with HNSW indexing | PebbleDB + HNSW |
| **Reranker** | Intelligent search result reranking with caching | Rule-based/LLM/RL + Redis |
| **Auth Service** | JWT-based authentication, API key management + Management Console | etcd + bcrypt + React |

## 📚 Documentation

### Quick Links
- [AI Onboarding Guide](AI_ONBOARDING.md) - **Start here for development**
- [TODO & Missing Features](TODO_AND_MISSING_FEATURES.md) - Current status & roadmap
- [Architecture Docs](docs/Vectron_Architecture.md) - System design details

### Service Documentation
- [API Gateway](docs/APIGateway_Service.md)
- [Placement Driver](docs/PlacementDriver_Service.md)
- [Worker](docs/Worker_Service.md)
- [Auth Service](docs/Auth_Service.md)
- [Feedback System](docs/Feedback_System.md)
- [Reranker Integration](docs/APIGateway_Reranker_Integration.md)
- [Reranker](reranker/README.md)

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              CLIENT                                      │
│                     (Go / Python / JavaScript)                          │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                         API GATEWAY                                      │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────┐ │
│  │ REST API        │  │ Feedback API    │  │ Reranker Integration    │ │
│  │ (/v1/...)       │  │ (/v1/feedback)  │  │ (gRPC to Reranker)      │ │
│  └─────────────────┘  └─────────────────┘  └─────────────────────────┘ │
└───────────────────────┬────────────────────────────────────────────────┘
                        │
        ┌───────────────┼───────────────┐
        ▼               ▼               ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│   WORKER     │ │  RERANKER    │ │   AUTH       │
│  (Vector DB) │ │ (Reranking)  │ │  (Frontend)  │
└──────────────┘ └──────────────┘ └──────────────┘
                        │
                        ▼
               ┌──────────────┐
               │  FEEDBACK    │
               │  (SQLite)    │
               └──────────────┘
                        │
                        ▼
               ┌─────────────────┐
               │ Placement Driver│
               │   (Raft Leader) │
               └────────┬────────┘
                        │
           ┌────────────┼────────────┐
           ▼            ▼            ▼
     ┌─────────┐  ┌─────────┐  ┌─────────┐
     │ Worker 1│  │ Worker 2│  │ Worker N│
     │(Shard A)│  │(Shard B)│  │(Shard C)│
     └─────────┘  └─────────┘  └─────────┘
```

## 💻 Client Libraries

### Go
```go
import "github.com/pavandhadge/vectron/clientlibs/go"

client := vectron.NewClient("localhost:8081", "your-jwt-token")
results, err := client.Search("my-collection", []float32{0.1, 0.2, ...}, 10)
```

### Python
```python
from vectron_client import VectronClient

client = VectronClient("localhost:8081", "your-jwt-token")
results = client.search("my-collection", [0.1, 0.2, ...], top_k=10)
```

### JavaScript
```typescript
import { VectronClient } from 'vectron-client';

const client = new VectronClient('localhost:8081', 'your-jwt-token');
const results = await client.search('my-collection', [0.1, 0.2, ...], 10);
```

## 📁 Project Structure

```
vectron/
├── apigateway/          # API Gateway service + Feedback system
├── placementdriver/     # Cluster coordinator
├── worker/              # Data nodes
├── reranker/            # Intelligent reranking service
│   ├── cmd/reranker/    # Main entry
│   └── internal/        # Rule strategies, cache
├── auth/                # Auth service + Management Console
│   ├── service/         # Go backend
│   └── frontend/        # React SPA with Management Dashboard
├── clientlibs/          # Official SDKs
│   ├── go/
│   ├── python/
│   └── js/
├── shared/proto/        # Protocol buffer definitions
├── docs/                # Documentation
├── AI_ONBOARDING.md     # AI development guide
└── TODO_AND_MISSING_FEATURES.md  # Status & roadmap
```

## 🔧 Development

### Prerequisites
- Go 1.24+
- Protocol Buffers compiler (protoc)
- Python 3.x (for Python client)
- Node.js (for frontend and JS client)

### Building

```bash
# Build all services
make build

# Build specific service
make build-apigateway
make build-worker
make build-placementdriver
make build-auth
make build-reranker

# Clean build artifacts
make clean
```

### Protocol Buffers

```bash
# Generate code for all languages
bash generate-all.sh

# Requires:
# - protoc-gen-go
# - protoc-gen-go-grpc
# - protoc-gen-grpc-gateway
# - grpcio-tools (Python)
```

### Testing

```bash
# Run all tests
go test ./...

# Run specific test
go test -v ./apigateway/...
go test -run TestIntegration

# E2E tests
go test -v e2e_test.go e2e_test_helpers_test.go
```

## ⚠️ Current Status

**Active Development**: Core services + Reranker + Management Console are functional. See [TODO_AND_MISSING_FEATURES.md](TODO_AND_MISSING_FEATURES.md) for detailed status.

### What's Working ✅
- Vector storage and similarity search (HNSW)
- Distributed Raft consensus (Placement Driver + Workers)
- JWT-based authentication with API key management
- **NEW**: Reranker service with rule-based reranking and caching
- **NEW**: Feedback system for result ratings (1-5 scale)
- **NEW**: Management Console for cluster monitoring

### Known Issues
- Broken test files need fixing (see TODO file)
- Auth system JWT implementation in progress
- Cross-shard search aggregation not yet implemented
- TLS/SSL support pending
- Management Console uses mock data (backend APIs needed)

### Next Priorities
1. Fix broken tests
2. Complete auth system
3. Add TLS support
4. Docker Compose setup
5. Backend APIs for Management Console
6. Cross-shard search

## 🤝 Contributing

1. Check [TODO_AND_MISSING_FEATURES.md](TODO_AND_MISSING_FEATURES.md) for priorities
2. Read [AI_ONBOARDING.md](AI_ONBOARDING.md) for codebase overview
3. Create a feature branch
4. Implement with tests
5. Submit a pull request

## 📄 License

MIT License - see LICENSE file for details

## 🔗 Links

- [Documentation](docs/)
- [Issues](https://github.com/pavandhadge/vectron/issues)
- [Discussions](https://github.com/pavandhadge/vectron/discussions)

---

**Need help?** Start with [AI_ONBOARDING.md](AI_ONBOARDING.md) for a comprehensive guide to the codebase.
