# Vectron AI Onboarding Guide

## Quick Overview

**Vectron** is a distributed vector database for high-performance similarity search, built with Go microservices using Raft consensus. Think of it as a self-hosted, scalable alternative to Pinecone or Weaviate.

**Architecture:** 3 core services + Auth
- **API Gateway** (`/apigateway/`) - Public entry point, JWT auth, rate limiting
- **Placement Driver** (`/placementdriver/`) - Cluster coordinator (Raft-based)
- **Worker** (`/worker/`) - Data nodes with HNSW indexing + PebbleDB storage
- **Auth Service** (`/auth/service/`) - User/API key management with etcd

---

## Project Structure

```
/home/pavan/Programming/vectron/
├── apigateway/              # Gateway service
│   ├── cmd/apigateway/      # main.go, config.go
│   └── internal/
│       ├── middleware/      # auth.go, ratelimit.go
│       └── translator/      # Protocol translation
├── placementdriver/         # Cluster coordinator
│   ├── cmd/placementdriver/ # main.go
│   └── internal/
│       ├── fsm/             # Finite state machine (Raft)
│       ├── server/          # gRPC handlers
│       └── raft/            # Raft node management
├── worker/                  # Data nodes
│   ├── cmd/worker/          # main.go
│   └── internal/
│       ├── shard/           # Shard state machine & manager
│       ├── storage/         # PebbleDB interface
│       └── idxhnsw/         # HNSW index implementation
├── auth/
│   ├── service/             # Go auth service
│   │   ├── cmd/auth/
│   │   └── internal/
│   │       ├── handler/     # gRPC handlers
│   │       ├── etcd/        # etcd client
│   │       └── middleware/  # JWT middleware
│   └── frontend/            # React TypeScript SPA
├── clientlibs/              # Official SDKs
│   ├── go/client.go         # Go client (291 lines)
│   ├── python/              # Python client
│   └── js/                  # JavaScript/TypeScript
├── shared/
│   └── proto/               # Protobuf definitions
│       ├── apigateway/
│       ├── auth/
│       ├── worker/
│       └── placementdriver/
└── docs/                    # Comprehensive documentation
```

---

## Key Files to Know

### Entry Points
- `/apigateway/cmd/apigateway/main.go` - Gateway service
- `/placementdriver/cmd/placementdriver/main.go` - Placement Driver
- `/worker/cmd/worker/main.go` - Worker service
- `/auth/service/cmd/auth/main.go` - Auth service

### Core Logic
- `/placementdriver/internal/fsm/fsm.go` - Cluster state management (467 lines)
- `/worker/internal/shard/state_machine.go` - Per-shard state (262 lines)
- `/worker/internal/idxhnsw/hnsw.go` - HNSW index (171 lines)
- `/auth/service/internal/handler/auth.go` - Auth handlers (314 lines)

### Protocol Buffers
- `/shared/proto/apigateway/apigateway.proto` - Public API
- `/shared/proto/placementdriver/placementdriver.proto` - Internal coordination
- `/shared/proto/worker/worker.proto` - Worker operations
- `/shared/proto/auth/auth.proto` - Auth operations

---

## Development Commands

```bash
# Build all services
make build

# Build specific service
make build-apigateway
make build-placementdriver
make build-worker
make build-auth

# Generate protobuf code
bash generate-all.sh

# Run services (example)
./bin/placementdriver --node-id=1 --grpc-addr=localhost:6001 --raft-addr=localhost:7001
./bin/worker --node-id=1 --grpc-addr=localhost:9090 --raft-addr=localhost:9191 --pd-addrs=localhost:6001
./bin/apigateway
./bin/authsvc
```

---

## Data Flow

1. **Client Request** → API Gateway (gRPC or HTTP)
2. **Auth Check** → JWT validation via AuthInterceptor
3. **Routing** → Query Placement Driver for worker addresses
4. **Execution** → Forward to appropriate Worker nodes
5. **Response** → Aggregate results (if needed) and return to client

---

## Key Concepts

### Raft Consensus
- Placement Driver uses Dragonboat for Raft (node-level)
- Workers use Multi-Raft (each shard is independent Raft group)
- Leader election, log replication, snapshotting

### Sharding
- Collections are split into shards
- Consistent hashing (FNV-64a) for routing
- Each shard has replicas with a leader

### HNSW Indexing
- In-memory ANN (Approximate Nearest Neighbor) search
- Config: M=16, EfConstruction=200, EfSearch=100
- Persisted to PebbleDB via WAL

### JWT Types
- **Login JWT** - Frontend sessions (24hr expiry, UserID + Plan)
- **SDK JWT** - API access (7 day expiry, contains APIKey ID)

---

## Environment Variables

### API Gateway
```
GRPC_ADDR=:8081
HTTP_ADDR=:8080
PLACEMENT_DRIVER=placement:6300
JWT_SECRET=CHANGE_ME_IN_PRODUCTION
AUTH_SERVICE_ADDR=auth:50051
```

### Placement Driver
```bash
./bin/placementdriver \
  --grpc-addr=localhost:6001 \
  --raft-addr=localhost:7001 \
  --node-id=1 \
  --cluster-id=1 \
  --initial-members=1:localhost:7001 \
  --data-dir=pd-data
```

### Worker
```bash
./bin/worker \
  --grpc-addr=localhost:9090 \
  --raft-addr=localhost:9191 \
  --pd-addrs=localhost:6001 \
  --node-id=1 \
  --data-dir=./worker-data
```

### Auth Service
```
GRPC_PORT=:8081
HTTP_PORT=:8082
ETCD_ENDPOINTS=localhost:2379
JWT_SECRET=<generated>
```

---

## Common Tasks

### Adding a New API Endpoint

1. **Update Proto** (`/shared/proto/apigateway/apigateway.proto`):
   ```protobuf
   rpc NewEndpoint(NewRequest) returns (NewResponse) {
     option (google.api.http) = {post: "/v1/new" body: "*"};
   }
   ```

2. **Regenerate Code**:
   ```bash
   bash generate-all.sh
   ```

3. **Implement Handler** (`/apigateway/internal/server/server.go`):
   ```go
   func (s *Server) NewEndpoint(ctx context.Context, req *pb.NewRequest) (*pb.NewResponse, error) {
       // Implementation
   }
   ```

4. **Update Client Libraries** (if needed):
   - `/clientlibs/go/client.go`
   - `/clientlibs/python/vectron_client/client.py`
   - `/clientlibs/js/src/index.ts`

### Adding a New Database Field

1. **Update FSM** (`/placementdriver/internal/fsm/fsm.go`):
   - Add to state struct
   - Update serialization methods

2. **Update Raft Commands**:
   - Add to command types
   - Handle in `Update()` method

3. **Update gRPC Handlers** (`/placementdriver/internal/server/server.go`)

### Adding Authentication to a New Service

1. Import auth interceptor from `/auth/service/internal/middleware/auth.go`
2. Validate JWT and extract user context
3. Check permissions based on Plan

---

## Testing

### E2E Tests
- `/e2e_test.go` - Basic E2E
- `/e2e_new_routes_test.go` - New routes (268 lines)
- `/e2e_full_test.go` - Full scenario

### Unit Tests
- `/auth/service/internal/handler/auth_test.go`
- `/apigateway/cmd/apigateway/integration_test.go`
- `/worker/worker_tests/storage_test.go`

### Running Tests
```bash
go test ./...
go test -v ./apigateway/...
go test -run TestIntegration
```

---

## Troubleshooting

### Raft Issues
- Check node connectivity: `netstat -tlnp | grep 7001`
- Verify initial-members list
- Check data directory permissions

### Worker Registration Fails
- Verify Placement Driver is running
- Check PD addresses in worker config
- Review logs for heartbeat failures

### Auth Issues
- Verify etcd is running: `etcd --listen-client-urls http://localhost:2379`
- Check JWT_SECRET matches across services
- Validate API key format

### Build Issues
```bash
# Regenerate protobufs
bash generate-all.sh

# Clean build
make clean && make build

# Update Go modules
cd <service> && go mod tidy
```

---

## Technology Stack

- **Language**: Go 1.24.0
- **Consensus**: Dragonboat (Raft)
- **Storage**: PebbleDB (LSTM key-value)
- **Search**: HNSW (in-memory ANN)
- **Auth**: JWT + bcrypt
- **Coordination**: etcd (for auth metadata)
- **Frontend**: React 18 + TypeScript + Vite
- **Protobuf**: Protocol Buffers with gRPC + grpc-gateway

---

## External Dependencies

- `github.com/lni/dragonboat/v4` - Raft consensus
- `github.com/cockroachdb/pebble` - Embedded storage
- `github.com/golang-jwt/jwt/v5` - JWT handling
- `go.etcd.io/etcd/server/v3` - etcd server
- `github.com/grpc-ecosystem/grpc-gateway/v2` - HTTP gateway

---

## Documentation

Full documentation in `/docs/`:
- `Vectron_Architecture.md` - System design
- `Worker_Service.md` - Worker internals
- `APIGateway_Service.md` - Gateway details
- `Auth_Service.md` - Auth implementation
- `PlacementDriver_Service.md` - PD internals

---

## Quick Reference

| Task | Command/File |
|------|--------------|
| Build all | `make build` |
| Generate protos | `bash generate-all.sh` |
| Run PD | `./bin/placementdriver --node-id=1 ...` |
| Run Worker | `./bin/worker --node-id=1 ...` |
| Run Gateway | `./bin/apigateway` |
| Run Auth | `./bin/authsvc` |
| Main gateway | `/apigateway/cmd/apigateway/main.go` |
| PD FSM | `/placementdriver/internal/fsm/fsm.go` |
| Worker storage | `/worker/internal/storage/storage.go` |
| Auth handlers | `/auth/service/internal/handler/auth.go` |
| Go client | `/clientlibs/go/client.go` |

---

## Support

- Check `/docs/` for detailed documentation
- Review test files for usage examples
- Look at existing handlers for implementation patterns
- Protobuf definitions are the source of truth for APIs
