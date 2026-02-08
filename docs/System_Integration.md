# System Integration (Current)

Last updated: 2026-02-08

This document describes how services currently integrate at runtime.

## 1. Core Runtime Graph

- Clients call API Gateway.
- API Gateway validates auth with Auth Service.
- API Gateway resolves placement/routing via Placement Driver.
- API Gateway forwards vector operations to Worker nodes.
- API Gateway optionally calls Reranker for post-search ranking.
- API Gateway writes feedback to SQLite.

## 2. Main Integration Contracts

Public API:

- `shared/proto/apigateway/apigateway.proto`

Auth integration:

- Gateway -> Auth Service gRPC
- contract: `shared/proto/auth/auth.proto`

Placement integration:

- Gateway + Workers -> PD gRPC
- contract: `shared/proto/placementdriver/placementdriver.proto`

Worker integration:

- Gateway -> Worker gRPC
- contract: `shared/proto/worker/worker.proto`

Reranker integration:

- Gateway -> Reranker gRPC
- contract: `shared/proto/reranker/reranker.proto`

## 3. Management Console Integration

Frontend (`auth/frontend`) calls API Gateway management routes:

- `GET /v1/system/health`
- `GET /v1/admin/stats`
- `GET /v1/admin/workers`
- `GET /v1/admin/collections`
- `GET /v1/alerts`
- `POST /v1/alerts/{id}/resolve`

Client-side integration code:

- `auth/frontend/src/services/managementApi.ts`

## 4. Deployment Notes

- Auth and API Gateway both expose gRPC and HTTP gateway ports.
- Worker and PD are gRPC-only services.
- Local startup order that minimizes transient errors:
  1. Placement Driver cluster
  2. Worker nodes
  3. Auth Service
  4. Reranker (if used)
  5. API Gateway
  6. Frontend

## 5. Source of Truth

- `docs/CURRENT_STATE.md`
- `*/cmd/*/main.go`
- `shared/proto/*`
