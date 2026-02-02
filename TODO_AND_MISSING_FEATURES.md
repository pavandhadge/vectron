# Vectron Development Status & TODOs

## Current Status

**Last Updated**: 2026-02-02

**Phase**: Active Development - Core services + Reranker + Management Console implemented

---

## Critical TODOs (High Priority)

### 1. JWT & Auth System Completion
**Status**: In Progress (Interrupted)

**Description**: Auth system needs completion for the new JWT-based architecture:
- Two JWT types: Login JWT (frontend) and SDK JWT (API access)
- Users get JWT tokens containing API keys (not direct API keys)
- Single user can have multiple API keys

**Files to Update**:
- `/e2e_new_routes_test.go` - Update E2E tests for new auth flows
- `/e2e_test_helpers_test.go` - Update test helpers
- Auth service handlers for JWT issuance

**Action Items**:
- [ ] Complete SDK JWT generation endpoint
- [ ] Update test files to use new auth flow
- [ ] Implement API key rotation mechanism
- [ ] Add JWT refresh token logic

### 2. Code Fixes
**Status**: Broken Tests Identified

**Issues Found**:
1. **Worker gRPC tests** (`/worker/worker_tests/grpc_test.go`):
   - Missing `worker.WorkerClient` type
   - Wrong function signature for `internal.NewGrpcServer`
   - Missing `worker.RegisterWorkerServer`

2. **Worker storage tests** (`/worker/worker_tests/storage_test.go`):
   - `db.Search` returns 3 values, tests expect 2
   - Signature mismatch

3. **Auth HTTP gateway tests** (`/auth/service/http_gateway_test.go`):
   - Cannot import `github.com/pavandhadge/vectron/auth/service/proto/auth`
   - `TestMain` redeclared

4. **Auth distributed tests** (`/auth/service/distributed_auth_test.go`):
   - `TestMain` redeclared (duplicate with http_gateway_test.go)

5. **PD integration test** (`/placementdriver/placementdrivertest/integration_test.go`):
   - Missing closing brace at EOF

**Action Items**:
- [ ] Fix worker proto imports and signatures
- [ ] Fix Search() return value handling in tests
- [ ] Consolidate or separate TestMain functions
- [ ] Fix PD integration test syntax

---

## New Services TODOs

### Reranker Service TODOs

#### R1. Docker & Kubernetes Setup
**Status**: Not Implemented
**Location**: `/reranker/`

**Action Items**:
- [ ] Create Dockerfile for reranker service
- [ ] Add to docker-compose.yml
- [ ] Create Kubernetes deployment manifests
- [ ] Add service discovery configuration

#### R2. Prometheus Metrics
**Status**: Not Implemented
**Location**: `/reranker/cmd/reranker/main.go`

**Action Items**:
- [ ] Add latency metrics (p50, p95, p99)
- [ ] Add cache hit/miss metrics
- [ ] Add strategy usage metrics
- [ ] Create Grafana dashboard

#### R3. LLM Strategy Implementation
**Status**: Planned
**Location**: `/reranker/internal/strategies/llm/`

**Action Items**:
- [ ] OpenAI client integration
- [ ] Grok/xAI client integration
- [ ] Prompt engineering for reranking
- [ ] Cost optimization with caching
- [ ] Fallback to rule-based on failures

#### R4. RL Strategy Implementation
**Status**: Planned
**Location**: `/reranker/internal/strategies/rl/`

**Action Items**:
- [ ] ONNX Runtime integration
- [ ] Model serving infrastructure
- [ ] Training pipeline setup
- [ ] A/B testing framework
- [ ] Model versioning

#### R5. A/B Testing Framework
**Status**: Not Implemented
**Location**: `/reranker/internal/strategy.go`

**Action Items**:
- [ ] Multi-armed bandit implementation
- [ ] Traffic splitting logic
- [ ] Conversion tracking
- [ ] Statistical significance testing
- [ ] Winner auto-promotion

### Management Console TODOs

#### M1. Backend Management APIs
**Status**: Not Implemented (Using Mock Data)
**Location**: Backend services

**Current**: Frontend uses mock data
**Needed**: Real REST/gRPC endpoints

**Action Items**:
- [ ] Create `/v1/admin/stats` endpoint in API Gateway
- [ ] Add management endpoints to Placement Driver
  - [ ] GET /admin/collections - List all collections with stats
  - [ ] GET /admin/workers - List all workers with health
  - [ ] POST /admin/collections/{name}/delete - Delete collection
  - [ ] GET /admin/metrics - System-wide metrics
- [ ] Add worker statistics endpoint
- [ ] Implement real-time metrics streaming (WebSockets/SSE)

#### M2. Authentication for Management Console
**Status**: Partial
**Location**: `/auth/frontend/src/pages/`

**Action Items**:
- [ ] Role-based access control (RBAC)
  - [ ] Admin role for management access
  - [ ] Regular user role (no management)
- [ ] Audit logging for management actions
- [ ] IP whitelisting for admin access

#### M3. Advanced Monitoring
**Status**: Not Implemented
**Location**: Management Console

**Action Items**:
- [ ] Real-time log viewer
- [ ] Query performance analytics
- [ ] Slow query identification
- [ ] Error rate tracking
- [ ] Alert configuration UI

---

## Important Missing Features (Medium Priority)

### 3. Cross-Shard Search Aggregation
**Status**: Not Implemented
**Location**: `/apigateway/internal/server/server.go`

**Current Behavior**: Search queries single shard only
**Expected**: Aggregate results across all shards in a collection

**Action Items**:
- [ ] Query all shards for collection
- [ ] Merge and rank results from multiple shards
- [ ] Handle partial failures gracefully

### 4. Configurable Replication Factor
**Status**: Hardcoded to 1
**Location**: `/placementdriver/internal/fsm/fsm.go:259`

**TODO Comment**: "TODO: Make this configurable per collection"

**Action Items**:
- [ ] Add `replication_factor` field to CreateCollectionRequest
- [ ] Update shard creation logic
- [ ] Implement replica placement strategy
- [ ] Handle replica failures and re-election

### 5. TLS/SSL Support
**Status**: Not Implemented
**Priority**: High for Production

**Action Items**:
- [ ] Add TLS configuration to all services
- [ ] Implement mTLS for service-to-service communication
- [ ] Certificate management
- [ ] Update client libraries to support TLS

### 6. Collection Deletion
**Status**: Not Implemented
**Location**: Placement Driver

**Action Items**:
- [ ] Add DeleteCollection RPC
- [ ] Implement shard cleanup
- [ ] Delete from worker nodes
- [ ] Clean up storage

### 7. User Deletion & Account Management
**Status**: Not Implemented
**Location**: Auth Service

**Action Items**:
- [ ] Add DeleteUser RPC
- [ ] Cascade delete API keys
- [ ] Handle orphaned data
- [ ] Add password reset flow

### 8. Automated Shard Rebalancing
**Status**: Not Implemented
**Location**: Placement Driver

**Action Items**:
- [ ] Monitor worker load
- [ ] Detect hotspots
- [ ] Implement shard migration
- [ ] Add Rebalance RPC implementation

---

## Infrastructure & Operations (Medium Priority)

### 9. Docker Compose Setup
**Status**: Missing

**Action Items**:
- [ ] Create `docker-compose.yml` with all services
- [ ] Add etcd container
- [ ] Configure networking
- [ ] Add volume mounts for persistence

### 10. Kubernetes Manifests
**Status**: Missing

**Action Items**:
- [ ] Create deployments for each service
- [ ] Add services and ingress
- [ ] Configure ConfigMaps and Secrets
- [ ] Add StatefulSet for Placement Driver

### 11. Distributed Rate Limiting
**Status**: In-Memory Only
**Location**: `/apigateway/internal/middleware/ratelimit.go`

**Current**: Per-instance rate limiting (100 RPS)
**Needed**: Cluster-wide rate limiting

**Action Items**:
- [ ] Redis-based rate limiter
- [ ] Or use etcd for coordination
- [ ] Per-user and per-API-key limits

### 12. Monitoring & Observability
**Status**: Not Implemented

**Action Items**:
- [ ] Prometheus metrics exporters
- [ ] Grafana dashboards
- [ ] Distributed tracing (OpenTelemetry/Jaeger)
- [ ] Health check endpoints
- [ ] Structured logging (JSON)

### 13. Backup & Restore Tools
**Status**: Not Implemented

**Action Items**:
- [ ] Worker data backup tool
- [ ] Placement Driver snapshot backup
- [ ] Point-in-time recovery
- [ ] Cross-region replication

---

## Enhancements (Low Priority)

### 14. Additional Distance Metrics
**Status**: Basic support only
**Location**: Worker HNSW implementation

**Current**: Cosine, Euclidean, Dot
**Add**: Hamming, Manhattan, Jaccard

### 15. Collection Metadata & Tags
**Status**: Not Implemented

**Action Items**:
- [ ] Add metadata field to collections
- [ ] Support for collection tagging
- [ ] Search/filter by metadata

### 16. Batch Operations
**Status**: Basic upsert only

**Action Items**:
- [ ] Batch delete API
- [ ] Batch get API
- [ ] Bulk import from file

### 17. Query Filtering
**Status**: Not Implemented

**Action Items**:
- [ ] Add payload-based filters
- [ ] Pre-filter before vector search
- [ ] Post-filter after vector search

### 18. Vector Compression
**Status**: Not Implemented

**Action Items**:
- [ ] PQ (Product Quantization) support
- [ ] Scalar quantization options
- [ ] Configurable compression levels

### 19. Multi-Tenant Isolation
**Status**: Basic plan separation only

**Action Items**:
- [ ] Namespace support
- [ ] Resource quotas per tenant
- [ ] Isolation guarantees

---

## Testing Gaps

### 20. Chaos Engineering
**Status**: Not Implemented

**Action Items**:
- [ ] Network partition tests
- [ ] Node failure simulations
- [ ] Split-brain scenarios
- [ ] Recovery testing

### 21. Load Testing
**Status**: Not Implemented

**Action Items**:
- [ ] k6 or Locust setup
- [ ] Benchmark suite
- [ ] Performance regression tests
- [ ] Latency profiling

### 22. Property-Based Tests
**Status**: Not Implemented

**Action Items**:
- [ ] Fuzz testing for vector operations
- [ ] Stateful property tests
- [ ] Consistency checks

---

## Documentation Gaps

### 23. API Documentation
**Status**: Partial

**Action Items**:
- [ ] OpenAPI spec improvements
- [ ] API usage examples
- [ ] Error code documentation
- [ ] Rate limit documentation

### 24. Deployment Guide
**Status**: Missing

**Action Items**:
- [ ] Production deployment checklist
- [ ] Capacity planning guide
- [ ] Scaling guidelines
- [ ] Disaster recovery procedures

### 25. Client Library Documentation
**Status**: Basic

**Action Items**:
- [ ] Go client examples
- [ ] Python client tutorial
- [ ] JavaScript/TypeScript guide
- [ ] Best practices for each language

---

## Security Enhancements

### 26. Security Hardening
**Status**: Basic

**Action Items**:
- [ ] Input validation improvements
- [ ] Rate limiting per IP
- [ ] DDoS protection
- [ ] Audit logging
- [ ] Penetration testing

### 27. API Key Rotation
**Status**: Not Implemented

**Action Items**:
- [ ] Key rotation endpoint
- [ ] Automatic rotation support
- [ ] Graceful key deprecation

### 28. RBAC (Role-Based Access Control)
**Status**: Basic (Free vs Paid plans only)

**Action Items**:
- [ ] Role definitions (Admin, User, ReadOnly)
- [ ] Permission system
- [ ] Resource-level ACLs

---

## Performance Optimizations

### 29. Worker Optimizations
**Status**: Opportunities Identified

**Action Items**:
- [ ] Vector caching layer
- [ ] Async HNSW index updates
- [ ] Memory-mapped file support
- [ ] SIMD optimizations

### 30. Gateway Optimizations
**Status**: Opportunities Identified

**Action Items**:
- [ ] Connection pooling improvements
- [ ] Request coalescing
- [ ] Response caching
- [ ] Circuit breakers

---

## CI/CD & Developer Experience

### 31. CI/CD Pipeline
**Status**: Missing

**Action Items**:
- [ ] GitHub Actions workflow
- [ ] Automated testing on PR
- [ ] Integration test automation
- [ ] Release automation

### 32. Development Environment
**Status**: Manual Setup

**Action Items**:
- [ ] Devcontainer setup
- [ ] Tilt or Skaffold for local k8s
- [ ] Hot reload for services
- [ ] Localstack for testing

---

## Quick Reference: What's Working

**Fully Functional**:
- ✅ Vector storage and retrieval
- ✅ HNSW similarity search
- ✅ Basic collection management
- ✅ User registration and login
- ✅ API key generation
- ✅ JWT-based authentication
- ✅ Worker registration and heartbeats
- ✅ Raft consensus (PD and Workers)
- ✅ gRPC and HTTP gateway
- ✅ Go, Python, JS client libraries

**Partially Working**:
- ⚠️ Auth system (needs test updates)
- ⚠️ Tests (multiple failures)
- ⚠️ Search (single shard only)

**Not Implemented**:
- ❌ Cross-shard aggregation
- ❌ TLS/SSL
- ❌ Docker Compose
- ❌ Kubernetes
- ❌ Monitoring
- ❌ Backup tools

---

## Priority Summary

| Priority | Items | Count |
|----------|-------|-------|
| **Critical** | Auth completion, Code fixes | 2 |
| **High** | TLS, Cross-shard search, Replication | 4 |
| **Medium** | Docker, K8s, Monitoring, Rebalancing | 6 |
| **Low** | Enhancements, Compression, Filtering | 8 |

---

## Next Steps Recommendation

1. **Fix broken tests** - Unblock development
2. **Complete auth system** - Finish interrupted work
3. **Implement TLS** - Required for production
4. **Add Docker Compose** - Improve dev experience
5. **Cross-shard search** - Critical feature gap
6. **Monitoring setup** - Production readiness

---

## How to Contribute

1. Pick an item from Critical or High priority
2. Create a feature branch
3. Implement with tests
4. Update documentation
5. Submit PR

**Questions?** Check `/docs/` or ask in the development channel.
