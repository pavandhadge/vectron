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



📊 Scenario 1: Vector Search Scalability
2026/02/09 11:25:33 Testing search performance with increasing dataset sizes
2026/02/09 11:25:33   Waiting for shards to be assigned and initialized...
2026/02/09 11:25:50
  ✅ Scalability Benchmark: 128D × 1000V
2026/02/09 11:25:50      Insert: 1000 vectors in 7.640146359s (131 vec/s)
2026/02/09 11:25:50      Search: 1000 queries in 7.651876728s (131 q/s)
2026/02/09 11:25:50      Latency: Avg=7.62026ms, P50=6.553364ms, P95=12.487716ms, P99=14.251067ms
2026/02/09 11:25:50      Memory: 0MB → 0MB (+0MB)
2026/02/09 11:25:50   Waiting for shards to be assigned and initialized...
2026/02/09 11:26:05
  ✅ Scalability Benchmark: 128D × 2500V
2026/02/09 11:26:05      Insert: 2500 vectors in 5.670633292s (441 vec/s)
2026/02/09 11:26:05      Search: 1000 queries in 7.470313136s (134 q/s)
2026/02/09 11:26:05      Latency: Avg=7.444067ms, P50=6.27837ms, P95=13.661374ms, P99=16.307615ms
2026/02/09 11:26:05      Memory: 0MB → 0MB (+0MB)
2026/02/09 11:26:05   Waiting for shards to be assigned and initialized...
2026/02/09 11:26:21
  ✅ Scalability Benchmark: 128D × 5000V
2026/02/09 11:26:21      Insert: 5000 vectors in 5.850523595s (855 vec/s)
2026/02/09 11:26:21      Search: 1000 queries in 7.512901986s (133 q/s)
2026/02/09 11:26:21      Latency: Avg=7.491303ms, P50=6.522237ms, P95=14.274514ms, P99=20.331474ms
2026/02/09 11:26:21      Memory: 0MB → 1MB (+1MB)
2026/02/09 11:26:21   Waiting for shards to be assigned and initialized...
2026/02/09 11:26:31   Insert progress: 10.0% (1000/10000 vectors)
2026/02/09 11:26:31   Insert progress: 20.0% (2000/10000 vectors)
2026/02/09 11:26:31   Insert progress: 30.0% (3000/10000 vectors)
2026/02/09 11:26:31   Insert progress: 40.0% (4000/10000 vectors)
2026/02/09 11:26:32   Insert progress: 50.0% (5000/10000 vectors)
2026/02/09 11:26:32   Insert progress: 60.0% (6000/10000 vectors)
2026/02/09 11:26:32   Insert progress: 70.0% (7000/10000 vectors)
2026/02/09 11:26:32   Insert progress: 80.0% (8000/10000 vectors)
2026/02/09 11:26:32   Insert progress: 90.0% (9000/10000 vectors)
2026/02/09 11:26:45
  ✅ Scalability Benchmark: 128D × 10000V
2026/02/09 11:26:45      Insert: 10000 vectors in 11.581983848s (863 vec/s)
2026/02/09 11:26:45      Search: 1000 queries in 10.819195719s (92 q/s)
2026/02/09 11:26:45      Latency: Avg=10.796607ms, P50=7.75486ms, P95=25.339629ms, P99=47.025023ms
2026/02/09 11:26:45      Memory: 1MB → 1MB (+0MB)
2026/02/09 11:26:45   Waiting for shards to be assigned and initialized...
2026/02/09 11:27:00
  ✅ Scalability Benchmark: 384D × 1000V
2026/02/09 11:27:00      Insert: 1000 vectors in 5.678825196s (176 vec/s)
2026/02/09 11:27:00      Search: 1000 queries in 7.036799614s (142 q/s)
2026/02/09 11:27:00      Latency: Avg=6.985528ms, P50=6.295931ms, P95=11.850842ms, P99=15.637689ms
2026/02/09 11:27:00      Memory: 1MB → 1MB (+0MB)
2026/02/09 11:27:00   Waiting for shards to be assigned and initialized...
2026/02/09 11:27:15
  ✅ Scalability Benchmark: 384D × 2500V
2026/02/09 11:27:15      Insert: 2500 vectors in 6.486911369s (385 vec/s)
2026/02/09 11:27:15      Search: 1000 queries in 6.421659933s (156 q/s)
2026/02/09 11:27:15      Latency: Avg=6.388256ms, P50=6.08875ms, P95=8.624509ms, P99=14.09695ms
2026/02/09 11:27:15      Memory: 1MB → 1MB (+0MB)
2026/02/09 11:27:15   Waiting for shards to be assigned and initialized...
2026/02/09 11:27:34
  ✅ Scalability Benchmark: 384D × 5000V
2026/02/09 11:27:34      Insert: 5000 vectors in 8.321501635s (601 vec/s)
2026/02/09 11:27:34      Search: 1000 queries in 8.518352795s (117 q/s)
2026/02/09 11:27:34      Latency: Avg=8.483116ms, P50=6.430446ms, P95=15.321115ms, P99=55.58022ms
2026/02/09 11:27:34      Memory: 1MB → 1MB (+0MB)
2026/02/09 11:27:34   Waiting for shards to be assigned and initialized...
2026/02/09 11:27:41   Insert progress: 10.0% (1000/10000 vectors)
2026/02/09 11:27:41   Insert progress: 20.0% (2000/10000 vectors)
2026/02/09 11:27:42   Insert progress: 30.0% (3000/10000 vectors)
2026/02/09 11:27:42   Insert progress: 40.0% (4000/10000 vectors)
2026/02/09 11:27:42   Insert progress: 50.0% (5000/10000 vectors)
2026/02/09 11:27:42   Insert progress: 60.0% (6000/10000 vectors)
2026/02/09 11:27:43   Insert progress: 70.0% (7000/10000 vectors)
2026/02/09 11:27:44   Insert progress: 80.0% (8000/10000 vectors)
2026/02/09 11:27:44   Insert progress: 90.0% (9000/10000 vectors)
2026/02/09 11:27:58
  ✅ Scalability Benchmark: 384D × 10000V
2026/02/09 11:27:58      Insert: 10000 vectors in 10.271501717s (974 vec/s)
2026/02/09 11:27:58      Search: 1000 queries in 12.234395804s (82 q/s)
2026/02/09 11:27:58      Latency: Avg=12.203657ms, P50=7.490382ms, P95=37.88399ms, P99=71.485767ms
2026/02/09 11:27:58      Memory: 1MB → 1MB (+0MB)
2026/02/09 11:27:58   Waiting for shards to be assigned and initialized...
2026/02/09 11:28:15
  ✅ Scalability Benchmark: 768D × 1000V
2026/02/09 11:28:15      Insert: 1000 vectors in 8.223715411s (122 vec/s)
2026/02/09 11:28:15      Search: 1000 queries in 6.282337371s (159 q/s)
2026/02/09 11:28:15      Latency: Avg=6.219338ms, P50=5.514497ms, P95=11.312789ms, P99=15.478175ms
2026/02/09 11:28:15      Memory: 1MB → 1MB (+0MB)
2026/02/09 11:28:15   Waiting for shards to be assigned and initialized...
2026/02/09 11:28:31
  ✅ Scalability Benchmark: 768D × 2500V
2026/02/09 11:28:31      Insert: 2500 vectors in 7.003726291s (357 vec/s)
2026/02/09 11:28:31      Search: 1000 queries in 6.940136602s (144 q/s)
2026/02/09 11:28:31      Latency: Avg=6.890919ms, P50=6.272373ms, P95=11.364014ms, P99=15.133859ms
2026/02/09 11:28:31      Memory: 1MB → 1MB (+0MB)
2026/02/09 11:28:31   Waiting for shards to be assigned and initialized...
2026/02/09 11:28:47
  ✅ Scalability Benchmark: 768D × 5000V
2026/02/09 11:28:47      Insert: 5000 vectors in 7.404161543s (675 vec/s)
2026/02/09 11:28:47      Search: 1000 queries in 7.538372639s (133 q/s)
2026/02/09 11:28:47      Latency: Avg=7.490297ms, P50=6.799736ms, P95=12.124774ms, P99=20.419392ms
2026/02/09 11:28:47      Memory: 1MB → 1MB (+0MB)
2026/02/09 11:28:47   Waiting for shards to be assigned and initialized...
2026/02/09 11:28:57   Insert progress: 10.0% (1000/10000 vectors)
2026/02/09 11:28:57   Insert progress: 20.0% (2000/10000 vectors)
2026/02/09 11:28:59   Insert progress: 30.0% (3000/10000 vectors)
2026/02/09 11:29:00   Insert progress: 40.0% (4000/10000 vectors)
2026/02/09 11:29:02   Insert progress: 50.0% (5000/10000 vectors)
2026/02/09 11:29:03   Insert progress: 60.0% (6000/10000 vectors)
2026/02/09 11:29:04   Insert progress: 70.0% (7000/10000 vectors)
2026/02/09 11:29:04   Insert progress: 80.0% (8000/10000 vectors)
2026/02/09 11:29:05   Insert progress: 90.0% (9000/10000 vectors)
2026/02/09 11:29:28
  ✅ Scalability Benchmark: 768D × 10000V
2026/02/09 11:29:28      Insert: 10000 vectors in 17.633474752s (567 vec/s)
2026/02/09 11:29:28      Search: 1000 queries in 21.100630976s (47 q/s)
2026/02/09 11:29:28      Latency: Avg=21.030985ms, P50=10.51372ms, P95=66.478281ms, P99=100.477376ms
2026/02/09 11:29:28      Memory: 1MB → 1MB (+0MB)
=== RUN   TestResearchBenchmark/Scenario2_DimensionImpact
2026/02/09 11:29:28 🔄 Resetting system between scenarios...
2026/02/09 11:29:28 Stopping process 4191...
2026/02/09 11:29:28 Stopping process 4203...
2026/02/09 11:29:28 Stopping process 4216...
2026/02/09 11:29:28 Stopping process 4230...
2026/02/09 11:29:29 Stopping process 4241...
2026/02/09 11:29:30 Stopping process 4255...
2026/02/09 11:29:30 Stopping process 4264...
2026/02/09 11:29:30 Stopping process 4333...
2026/02/09 11:29:30 Removing data directory: /home/pavan/Programming/vectron/temp_vectron_benchmark/data/pd_benchmark_1_2377646603
2026/02/09 11:29:30 Removing data directory: /home/pavan/Programming/vectron/temp_vectron_benchmark/data/pd_benchmark_2_3644846090
2026/02/09 11:29:30 Removing data directory: /home/pavan/Programming/vectron/temp_vectron_benchmark/data/pd_benchmark_3_4202729035
2026/02/09 11:29:30 Removing data directory: /home/pavan/Programming/vectron/temp_vectron_benchmark/data/worker_benchmark_1_217067797
2026/02/09 11:29:30 Removing data directory: /home/pavan/Programming/vectron/temp_vectron_benchmark/data/worker_benchmark_2_3878953637
2026/02/09 11:29:30 Removing data directory: /home/pavan/Programming/vectron/temp_vectron_benchmark/data
2026/02/09 11:29:30 🚀 Starting System for Benchmark...
=== RUN   TestResearchBenchmark/Scenario2_DimensionImpact/StartAllServices
2026/02/09 11:29:30 ▶️ Starting etcd (Podman)...
2026/02/09 11:29:31 ⏳ Waiting for etcd to be ready...
2026/02/09 11:29:31 ✅ Etcd started and listening
2026/02/09 11:29:32 Started PD node 1 (PID: 6777) with data dir /home/pavan/Programming/vectron/temp_vectron_benchmark/data/pd_benchmark_1_3927508394
2026/02/09 11:29:32 Started PD node 2 (PID: 6790) with data dir /home/pavan/Programming/vectron/temp_vectron_benchmark/data/pd_benchmark_2_2923801102
2026/02/09 11:29:33 Started PD node 3 (PID: 6804) with data dir /home/pavan/Programming/vectron/temp_vectron_benchmark/data/pd_benchmark_3_1474401416
2026/02/09 11:29:34 Started worker 1 (PID: 6819) with data dir /home/pavan/Programming/vectron/temp_vectron_benchmark/data/worker_benchmark_1_3487558181
2026/02/09 11:29:34 Started worker 2 (PID: 6831) with data dir /home/pavan/Programming/vectron/temp_vectron_benchmark/data/worker_benchmark_2_4089242265
2026/02/09 11:29:35 Started auth service (PID: 6845)
2026/02/09 11:29:36 Started reranker (PID: 6854)
2026/02/09 11:29:36 ▶️ Starting Valkey (Podman)...
2026/02/09 11:29:36 ⏳ Waiting for Valkey to be ready...
2026/02/09 11:29:36 ✅ Valkey started and listening
2026/02/09 11:29:37 Started API gateway (PID: 6980)
2026/02/09 11:29:38 ✅ All services started and healthy
=== RUN   TestResearchBenchmark/Scenario2_DimensionImpact/InitializeClients
2026/02/09 11:29:38 ✅ gRPC clients initialized
2026/02/09 11:29:38 🔐 Authenticating for Benchmark...
2026/02/09 11:29:38 ✅ JWT token obtained for API Gateway authentication
2026/02/09 11:29:38
📏 Scenario 2: Dimension Impact Analysis
2026/02/09 11:29:38 Testing how vector dimension affects performance
2026/02/09 11:29:38   Waiting for shards to be assigned and initialized...
2026/02/09 11:29:56
  ✅ Dimension Benchmark: 64D
2026/02/09 11:29:56      Insert Throughput: 2434 vectors/sec
2026/02/09 11:29:56      Search (top-1): P50=23.147381ms, P95=58.831154ms
2026/02/09 11:29:56      Search (top-5): P50=7.534178ms, P95=20.757289ms
2026/02/09 11:29:56      Search (top-10): P50=5.586908ms, P95=6.836786ms
2026/02/09 11:29:56      Search (top-50): P50=9.135713ms, P95=18.473027ms
2026/02/09 11:29:56      Search (top-100): P50=10.560478ms, P95=16.099958ms
2026/02/09 11:29:56   Waiting for shards to be assigned and initialized...
2026/02/09 11:30:16
  ✅ Dimension Benchmark: 128D
2026/02/09 11:30:16      Insert Throughput: 2451 vectors/sec
2026/02/09 11:30:16      Search (top-1): P50=27.48196ms, P95=56.15268ms
2026/02/09 11:30:16      Search (top-5): P50=12.212579ms, P95=21.870712ms
2026/02/09 11:30:16      Search (top-10): P50=6.391576ms, P95=10.74011ms
2026/02/09 11:30:16      Search (top-50): P50=9.496932ms, P95=11.761109ms
2026/02/09 11:30:16      Search (top-100): P50=11.295919ms, P95=15.466833ms
2026/02/09 11:30:16   Waiting for shards to be assigned and initialized...
2026/02/09 11:30:37
  ✅ Dimension Benchmark: 256D
2026/02/09 11:30:37      Insert Throughput: 2077 vectors/sec
2026/02/09 11:30:37      Search (top-1): P50=28.035106ms, P95=57.293598ms
2026/02/09 11:30:37      Search (top-5): P50=11.306367ms, P95=17.411292ms
2026/02/09 11:30:37      Search (top-10): P50=6.402889ms, P95=9.696697ms
2026/02/09 11:30:37      Search (top-50): P50=10.169982ms, P95=16.28036ms
2026/02/09 11:30:37      Search (top-100): P50=11.634123ms, P95=13.551196ms
2026/02/09 11:30:37   Waiting for shards to be assigned and initialized...
2026/02/09 11:30:59
  ✅ Dimension Benchmark: 384D
2026/02/09 11:30:59      Insert Throughput: 1791 vectors/sec
2026/02/09 11:30:59      Search (top-1): P50=28.144614ms, P95=49.211183ms
2026/02/09 11:30:59      Search (top-5): P50=14.197982ms, P95=23.193359ms
2026/02/09 11:30:59      Search (top-10): P50=8.406289ms, P95=13.242169ms
2026/02/09 11:30:59      Search (top-50): P50=9.917008ms, P95=11.387001ms
2026/02/09 11:30:59      Search (top-100): P50=11.922762ms, P95=13.503587ms
2026/02/09 11:30:59   Waiting for shards to be assigned and initialized...
2026/02/09 11:31:19
  ✅ Dimension Benchmark: 512D
2026/02/09 11:31:19      Insert Throughput: 1715 vectors/sec
2026/02/09 11:31:19      Search (top-1): P50=18.107032ms, P95=44.567708ms
2026/02/09 11:31:19      Search (top-5): P50=16.702674ms, P95=24.405069ms
2026/02/09 11:31:19      Search (top-10): P50=9.913246ms, P95=15.95805ms
2026/02/09 11:31:19      Search (top-50): P50=9.70928ms, P95=18.335985ms
2026/02/09 11:31:19      Search (top-100): P50=11.951908ms, P95=14.158107ms
2026/02/09 11:31:19   Waiting for shards to be assigned and initialized...
2026/02/09 11:31:42
  ✅ Dimension Benchmark: 768D
2026/02/09 11:31:42      Insert Throughput: 1460 vectors/sec
2026/02/09 11:31:42      Search (top-1): P50=30.156192ms, P95=55.071077ms
2026/02/09 11:31:42      Search (top-5): P50=22.178465ms, P95=77.966265ms
2026/02/09 11:31:42      Search (top-10): P50=7.403991ms, P95=10.516036ms
2026/02/09 11:31:42      Search (top-50): P50=11.716335ms, P95=21.007298ms
2026/02/09 11:31:42      Search (top-100): P50=14.900137ms, P95=22.812734ms
2026/02/09 11:31:42   Waiting for shards to be assigned and initialized...
2026/02/09 11:32:11
  ✅ Dimension Benchmark: 1024D
2026/02/09 11:32:11      Insert Throughput: 1174 vectors/sec
2026/02/09 11:32:11      Search (top-1): P50=46.10547ms, P95=82.469619ms
2026/02/09 11:32:11      Search (top-5): P50=21.663873ms, P95=51.125772ms
2026/02/09 11:32:11      Search (top-10): P50=11.97448ms, P95=21.110893ms
2026/02/09 11:32:11      Search (top-50): P50=11.438966ms, P95=14.393382ms
2026/02/09 11:32:11      Search (top-100): P50=15.617161ms, P95=31.237431ms

🔄 Scenario 3: Concurrent Workload Analysis
2026/02/09 11:34:38 Testing system behavior under concurrent load
2026/02/09 11:34:38   Waiting for shards to be assigned and initialized...
2026/02/09 11:35:26
  ✅ Concurrent Benchmark: 1 clients
2026/02/09 11:35:26      Total Queries: 563
2026/02/09 11:35:26      Throughput: 19 queries/sec
2026/02/09 11:35:26      Latency: Avg=53.136011ms, P50=50.449991ms, P95=78.178514ms, P99=110.009469ms
2026/02/09 11:35:56
  ✅ Concurrent Benchmark: 5 clients
2026/02/09 11:35:56      Total Queries: 1739
2026/02/09 11:35:56      Throughput: 58 queries/sec
2026/02/09 11:35:56      Latency: Avg=86.251007ms, P50=81.824148ms, P95=127.445149ms, P99=166.959938ms
2026/02/09 11:36:26
  ✅ Concurrent Benchmark: 10 clients
2026/02/09 11:36:26      Total Queries: 3111
2026/02/09 11:36:26      Throughput: 104 queries/sec
2026/02/09 11:36:26      Latency: Avg=96.218585ms, P50=90.559349ms, P95=157.875546ms, P99=219.064546ms
2026/02/09 11:36:56
  ✅ Concurrent Benchmark: 20 clients
2026/02/09 11:36:56      Total Queries: 6535
2026/02/09 11:36:56      Throughput: 218 queries/sec
2026/02/09 11:36:56      Latency: Avg=91.782325ms, P50=90.569122ms, P95=135.772074ms, P99=158.36524ms
2026/02/09 11:37:26
  ✅ Concurrent Benchmark: 50 clients
2026/02/09 11:37:26      Total Queries: 7735
2026/02/09 11:37:26      Throughput: 258 queries/sec
2026/02/09 11:37:26      Latency: Avg=193.110162ms, P50=196.574719ms, P95=259.314337ms, P99=297.847993ms
2026/02/09 11:37:56
  ✅ Concurrent Benchmark: 100 clients
2026/02/09 11:37:56      Total Queries: 7838
2026/02/09 11:37:56      Throughput: 261 queries/sec
2026/02/09 11:37:56      Latency: Avg=378.801773ms, P50=385.06342ms, P95=501.431293ms, P99=572.283348ms
=== RUN   TestResearchBenchmark/Scenario5_DistributedScalability
2026/02/09 11:37:56 🔄 Resetting system between scenarios...
2026/02/09 11:37:56 Stopping process 10760...
2026/02/09 11:37:56 Stopping process 10774...
2026/02/09 11:37:56 Stopping process 10789...
2026/02/09 11:37:56 Stopping process 10802...
2026/02/09 11:37:56 Stopping process 10816...
2026/02/09 11:37:56 Stopping process 10830...
2026/02/09 11:37:56 Stopping process 10839...
2026/02/09 11:37:56 Stopping process 10909...
2026/02/09 11:37:56 Removing data directory: /home/pavan/Programming/vectron/temp_vectron_benchmark/data/pd_benchmark_1_1103340845
2026/02/09 11:37:56 Removing data directory: /home/pavan/Programming/vectron/temp_vectron_benchmark/data/pd_benchmark_2_1440461973
2026/02/09 11:37:56 Removing data directory: /home/pavan/Programming/vectron/temp_vectron_benchmark/data/pd_benchmark_3_3646727634
2026/02/09 11:37:56 Removing data directory: /home/pavan/Programming/vectron/temp_vectron_benchmark/data/worker_benchmark_1_2867409111
2026/02/09 11:37:56 Removing data directory: /home/pavan/Programming/vectron/temp_vectron_benchmark/data/worker_benchmark_2_421025777
2026/02/09 11:37:56 Removing data directory: /home/pavan/Programming/vectron/temp_vectron_benchmark/data
2026/02/09 11:37:56 🚀 Starting System for Benchmark...
=== RUN   TestResearchBenchmark/Scenario5_DistributedScalability/StartAllServices
2026/02/09 11:37:56 ▶️ Starting etcd (Podman)...
2026/02/09 11:37:57 ⏳ Waiting for etcd to be ready...
2026/02/09 11:37:57 ✅ Etcd started and listening
2026/02/09 11:37:58 Started PD node 1 (PID: 12141) with data dir /home/pavan/Programming/vectron/temp_vectron_benchmark/data/pd_benchmark_1_1349627347
2026/02/09 11:37:58 Started PD node 2 (PID: 12155) with data dir /home/pavan/Programming/vectron/temp_vectron_benchmark/data/pd_benchmark_2_861588065
2026/02/09 11:37:59 Started PD node 3 (PID: 12168) with data dir /home/pavan/Programming/vectron/temp_vectron_benchmark/data/pd_benchmark_3_2842195530
2026/02/09 11:38:00 Started worker 1 (PID: 12183) with data dir /home/pavan/Programming/vectron/temp_vectron_benchmark/data/worker_benchmark_1_1860033861
2026/02/09 11:38:00 Started worker 2 (PID: 12197) with data dir /home/pavan/Programming/vectron/temp_vectron_benchmark/data/worker_benchmark_2_3827034230
2026/02/09 11:38:01 Started auth service (PID: 12209)
2026/02/09 11:38:02 Started reranker (PID: 12217)
2026/02/09 11:38:02 ▶️ Starting Valkey (Podman)...
2026/02/09 11:38:02 ⏳ Waiting for Valkey to be ready...
2026/02/09 11:38:02 ✅ Valkey started and listening
2026/02/09 11:38:03 Started API gateway (PID: 12291)
2026/02/09 11:38:04 ✅ All services started and healthy
=== RUN   TestResearchBenchmark/Scenario5_DistributedScalability/InitializeClients
2026/02/09 11:38:04 ✅ gRPC clients initialized
2026/02/09 11:38:04 🔐 Authenticating for Benchmark...
2026/02/09 11:38:04 ✅ JWT token obtained for API Gateway authentication
2026/02/09 11:38:04
🌐 Scenario 5: Distributed System Scalability
2026/02/09 11:38:04 Analyzing shard distribution and load balancing
2026/02/09 11:38:04   Waiting for shards to be assigned and initialized...
2026/02/09 11:38:11
  ✅ Distributed Benchmark: 1000 vectors
2026/02/09 11:38:11      Shards: 8 across 2 workers
2026/02/09 11:38:11      Worker 1: 8 shards
2026/02/09 11:38:11      Worker 2: 8 shards
2026/02/09 11:38:11      Load variance (CV): 0.00
2026/02/09 11:38:11   Waiting for shards to be assigned and initialized...
2026/02/09 11:38:22
  ✅ Distributed Benchmark: 5000 vectors
2026/02/09 11:38:22      Shards: 8 across 2 workers
2026/02/09 11:38:22      Worker 2: 8 shards
2026/02/09 11:38:22      Worker 1: 8 shards
2026/02/09 11:38:22      Load variance (CV): 0.00
2026/02/09 11:38:22   Waiting for shards to be assigned and initialized...
2026/02/09 11:38:33
  ✅ Distributed Benchmark: 10000 vectors
2026/02/09 11:38:33      Shards: 8 across 2 workers
2026/02/09 11:38:33      Worker 1: 8 shards
2026/02/09 11:38:33      Worker 2: 8 shards
2026/02/09 11:38:33      Load variance (CV): 0.00
2026/02/09 11:38:33   Waiting for shards to be assigned and initialized...
2026/02/09 11:38:52
  ✅ Distributed Benchmark: 50000 vectors
2026/02/09 11:38:52      Shards: 8 across 2 workers
2026/02/09 11:38:52      Worker 1: 8 shards
2026/02/09 11:38:52      Worker 2: 8 shards
2026/02/09 11:38:52      Load variance (CV): 0.00