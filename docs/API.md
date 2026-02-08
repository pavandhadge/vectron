# API Reference (Current)

Last updated: 2026-02-08

This document summarizes the currently implemented API contracts.

Source of truth:

- `shared/proto/apigateway/apigateway.proto`
- `shared/proto/auth/auth.proto`
- `shared/proto/placementdriver/placementdriver.proto`
- `shared/proto/worker/worker.proto`
- `shared/proto/reranker/reranker.proto`

## 1. Public API (API Gateway)

Service: `vectron.v1.VectronService`

RPCs:

- `CreateCollection`
- `DeleteCollection`
- `Upsert`
- `Search`
- `SearchStream`
- `Get`
- `Delete`
- `ListCollections`
- `GetCollectionStatus`
- `UpdateUserProfile`
- `SubmitFeedback`

HTTP routes:

- `POST /v1/collections`
- `DELETE /v1/collections/{name}`
- `POST /v1/collections/{collection}/points`
- `POST /v1/collections/{collection}/points/search`
- `GET /v1/collections/{collection}/points/{id}`
- `DELETE /v1/collections/{collection}/points/{id}`
- `GET /v1/collections`
- `GET /v1/collections/{name}/status`
- `PUT /v1/user/profile`
- `POST /v1/feedback`

## 2. Auth API

Service: `vectron.auth.v1.AuthService`

RPCs:

- `RegisterUser`, `Login`, `GetUserProfile`, `UpdateUserProfile`, `DeleteUser`, `RefreshToken`
- `CreateAPIKey`, `ListAPIKeys`, `DeleteAPIKey`
- `ValidateAPIKey`, `CreateSDKJWT`, `GetAuthDetailsForSDK`

Primary HTTP routes:

- `POST /v1/users/register`
- `POST /v1/users/login`
- `GET /v1/user/profile`
- `PUT /v1/user/profile`
- `POST /v1/user/delete`
- `POST /v1/auth/refresh`
- `POST /v1/keys`
- `GET /v1/keys`
- `DELETE /v1/keys/{key_prefix}`
- `POST /v1/keys/validate`
- `POST /v1/sdk-jwt`

## 3. Control Plane (Placement Driver)

Service: `placementdriver.PlacementService`

- worker lifecycle: `RegisterWorker`, `Heartbeat`, `ListWorkers`, `DrainWorker`, `RemoveWorker`
- routing/control: `GetWorker`, `ListWorkersForCollection`, `Rebalance`
- collections: `CreateCollection`, `ListCollections`, `DeleteCollection`, `GetCollectionStatus`
- leader info: `GetLeader`

## 4. Data Plane (Worker)

Service: `worker.WorkerService`

- writes: `StoreVector`, `BatchStoreVector`, `StreamBatchStoreVector`
- reads/search: `GetVector`, `DeleteVector`, `Search`, `BatchSearch`
- sync streams: `StreamHNSWSnapshot`, `StreamHNSWUpdates`
- utility endpoints: `Put`, `Get`, `Delete`, `Status`, `Flush`

## 5. Reranker API

Service: `reranker.RerankService`

- `Rerank`
- `GetStrategy`
- `InvalidateCache`

## 6. Notes

- Generated OpenAPI specs exist at:
  - `shared/proto/apigateway/openapi/apigateway.swagger.json`
  - `clientlibs/go/proto/apigateway/openapi/apigateway.swagger.json`
- If this file diverges from `.proto` files, treat `.proto` as authoritative.
