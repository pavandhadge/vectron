# Feedback System (Current)

Last updated: 2026-02-08

## 1. What Exists Today

Feedback ingestion is implemented in API Gateway.

- public API endpoint: `SubmitFeedback`
- HTTP route: `POST /v1/feedback`
- storage: SQLite DB managed by gateway feedback service

Primary code:

- `apigateway/internal/feedback/service.go`
- `apigateway/internal/feedback/schema.sql`
- `apigateway/cmd/apigateway/main.go`
- `shared/proto/apigateway/apigateway.proto`

## 2. Request Shape

From `apigateway.proto`:

- `collection`
- optional `query`
- `result_ids`
- repeated `feedback_items` including:
  - `result_id`
  - `relevance_score` (1-5)
  - `clicked`
  - `position`
  - optional `comment`
- optional contextual key-value map

## 3. Current Behavior

- Gateway validates and stores feedback entries in SQLite.
- Feedback API is available independently of reranker enablement.
- No automatic online model training is performed in this repository from feedback writes.

## 4. Operational Notes

- Set `FEEDBACK_DB_PATH` in gateway config for persistence location.
- Ensure data directory exists and is writable where gateway runs.

## 5. Source of Truth

- `shared/proto/apigateway/apigateway.proto`
- `apigateway/internal/feedback/*`
- `apigateway/cmd/apigateway/main.go`
