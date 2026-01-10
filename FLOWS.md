# Vectron Service Request Flows

This document outlines the typical request flows through the Vectron services (API Gateway, Auth Service, Placement Driver, and Worker Service), highlighting key interactions and identified areas for improvement or potential issues.

## 1. User Registration (Client -> API Gateway -> Auth Service)

**Flow:**
1.  **Client** sends `RegisterUserRequest` (email, password) to **API Gateway** (HTTP POST `/v1/users/register`).
2.  **API Gateway** forwards the request to **Auth Service** (gRPC `AuthService/RegisterUser`).
3.  **Auth Service (Handler)** receives the request.
    *   Validates input.
    *   Calls `etcdclient.Client.CreateUser` (email, password).
    *   **Auth Service (Etcd Client)**:
        *   Checks if user exists by email (calls `GetUserByEmail`).
        *   Hashes password using `bcrypt.DefaultCost`.
        *   Generates UUID for user ID.
        *   Marshals `UserData` to JSON.
        *   Stores `UserData` in etcd (`vectron/users/<email>`).
    *   Returns `RegisterUserResponse` (user info).
4.  **API Gateway** receives response and sends it back to **Client**.

**Identified Points:**
*   `AuthService/RegisterUser` is correctly marked as unprotected (no JWT required) in Auth Service's JWT middleware.

## 2. User Login (Client -> API Gateway -> Auth Service)

**Flow:**
1.  **Client** sends `LoginRequest` (email, password) to **API Gateway** (HTTP POST `/v1/users/login`).
2.  **API Gateway** forwards the request to **Auth Service** (gRPC `AuthService/Login`).
3.  **Auth Service (Handler)** receives the request.
    *   Calls `etcdclient.Client.GetUserByEmail` (email).
    *   **Auth Service (Etcd Client)**: Retrieves `UserData` by email.
    *   Compares provided password with `HashedPassword` from `UserData` using `bcrypt.CompareHashAndPassword`.
    *   If valid, generates a JWT token with `UserID` claim and `ExpiresAt`.
    *   Returns `LoginResponse` (JWT token, user info).
4.  **API Gateway** receives response and sends it back to **Client**.

**Identified Points:**
*   `AuthService/Login` is correctly marked as unprotected (no JWT required) in Auth Service's JWT middleware.

## 3. Create API Key (Client -> API Gateway -> Auth Service)

**Flow:**
1.  **Client** sends `CreateAPIKeyRequest` (key name) with `Authorization: Bearer <JWT>` to **API Gateway** (HTTP POST `/v1/keys`).
2.  **API Gateway (Auth Middleware)**:
    *   Extracts JWT from `Authorization` header.
    *   Calls `AuthService/ValidateAPIKey` (full API key from the header).
    *   **This is a logical error in the description for CreateAPIKey - API Gateway's Auth Middleware validates the *session JWT*, not an API key, for `CreateAPIKey` request.**
    *   Injects `UserID`, `Plan`, `APIKeyID` (from the *session JWT*) into context.
3.  **API Gateway** forwards request to **Auth Service** (gRPC `AuthService/CreateAPIKey`).
4.  **Auth Service (JWT Middleware)**:
    *   Validates session JWT token.
    *   Extracts `UserID` from JWT and injects into context.
5.  **Auth Service (Handler)** receives request.
    *   Retrieves `UserID` from context (`middleware.GetUserIDFromContext`).
    *   Validates input (`Name`).
    *   Calls `etcdclient.Client.CreateAPIKey` (userID, name).
    *   **Auth Service (Etcd Client)**:
        *   Generates a new `fullKey` (UUID-based).
        *   Hashes `fullKey` using `bcrypt.DefaultCost`.
        *   Derives `KeyPrefix` from `fullKey`.
        *   Stores `APIKeyData` (including `HashedKey`, `UserID`, `Name`, `Plan`, `KeyPrefix`) in etcd (`vectron/apikeys/<key_prefix>`).
    *   Returns `CreateAPIKeyResponse` (full key, key info).
6.  **API Gateway** receives response and sends it back to **Client**.

**Identified Points:**
*   The API Gateway's Auth Middleware *should not* call `AuthService/ValidateAPIKey` for this RPC. Instead, it should validate the session JWT, which is handled implicitly by the `AuthInterceptor` in the API Gateway. The flow description needs correction.
*   The `AuthService/CreateAPIKey` RPC is protected by the Auth Service's JWT middleware.

## 4. Upsert Vectors (Client -> API Gateway -> Placement Driver -> Worker Service)

**Flow:**
1.  **Client** sends `UpsertRequest` (collection name, points with ID, vector, payload) with `Authorization: Bearer <API_KEY>` to **API Gateway** (HTTP POST `/v1/collections/{collection}/points`).
2.  **API Gateway (Auth Middleware)**:
    *   Extracts API key from `Authorization` header.
    *   Calls `AuthService/ValidateAPIKey` (full API key).
    *   **Auth Service (Handler)** receives and validates the API key via `etcdclient.Client.ValidateAPIKey`.
    *   **Auth Service (Etcd Client)**: Retrieves `APIKeyData` by `KeyPrefix`, compares hashed key. Returns `valid`, `UserID`, `Plan`, `APIKeyID`.
    *   Injects `UserID`, `Plan`, `APIKeyID` into context.
3.  **API Gateway (Handler - `Upsert` RPC)**:
    *   Validates input.
    *   Iterates through each `Point` in the request.
    *   For each `Point`, calls `forwardToWorker` (ctx, collection, point.Id, callback).
4.  **API Gateway (`forwardToWorker`)**:
    *   Calls `s.getPlacementClient()` to get a connection to the **Placement Driver**.
    *   **Placement Driver** (`PlacementService/GetWorker`) returns `grpc_address` and `shard_id` for the worker responsible for the `collection` and `point.Id`.
    *   Connects to the **Worker Service**.
    *   Executes a callback function with `workerpb.WorkerServiceClient`.
    *   **Callback**: Calls `translator.ToWorkerStoreVectorRequestFromPoint`.
        *   **CRITICAL FUNCTIONAL FLAW**: `translator.ToWorkerStoreVectorRequestFromPoint` sets `workerpb.Vector.Metadata` to `nil`. The `apigatewaypb.Point.Payload` (map[string]string) is **LOST** here.
5.  **Worker Service** (`WorkerService/StoreVector`) receives the request.
    *   Stores the vector (without payload).
6.  **API Gateway** aggregates results and returns `UpsertResponse`.

**Identified Points:**
*   **Critical Functional Flaw:** `apigateway/internal/translator/translator.go` drops the `payload` during `Upsert` requests.

## 5. Get Vector (Client -> API Gateway -> Placement Driver -> Worker Service)

**Flow:**
1.  **Client** sends `GetRequest` (collection name, ID) with `Authorization: Bearer <API_KEY>` to **API Gateway** (HTTP GET `/v1/collections/{collection}/points/{id}`).
2.  **API Gateway (Auth Middleware)**: Same as Upsert (validates API key, injects user info).
3.  **API Gateway (Handler - `Get` RPC)**:
    *   Calls `forwardToWorker` (ctx, collection, req.Id, callback).
4.  **API Gateway (`forwardToWorker`)**:
    *   Calls **Placement Driver** (`PlacementService/GetWorker`) to get worker address and `shard_id`.
    *   Connects to **Worker Service**.
    *   Executes callback.
    *   **Callback**: Calls `translator.ToWorkerGetVectorRequest` and then `workerpb.WorkerServiceClient.GetVector`.
    *   **Callback (after Worker response)**: Calls `translator.FromWorkerGetVectorResponse`.
        *   **CRITICAL FUNCTIONAL FLAW**: `translator.FromWorkerGetVectorResponse` sets `apigatewaypb.Point.Payload` to `nil`. The `workerpb.Vector.Metadata` (which is `nil` anyway due to the Upsert flaw) is **NOT translated** back, even if it contained data.
5.  **Worker Service** (`WorkerService/GetVector`) retrieves the vector (without metadata).
6.  **API Gateway** returns `GetResponse` (point with ID, vector, but **empty payload**).

**Identified Points:**
*   **Critical Functional Flaw:** `apigateway/internal/translator/translator.go` does not translate `metadata` back to `payload` during `Get` requests, resulting in empty payloads.

## 6. Search Vectors (Client -> API Gateway -> Placement Driver -> Worker Service)

**Flow:**
1.  **Client** sends `SearchRequest` (collection, vector, top_k) with `Authorization: Bearer <API_KEY>` to **API Gateway** (HTTP POST `/v1/collections/{collection}/points/search`).
2.  **API Gateway (Auth Middleware)**: Same as Upsert (validates API key, injects user info).
3.  **API Gateway (Handler - `Search` RPC)**:
    *   Validates input.
    *   Sets `TopK` default if not provided.
    *   Calls `forwardToWorker` (ctx, collection, "", callback). Note: `vectorID` is empty for search, allowing PD to pick any suitable worker.
4.  **API Gateway (`forwardToWorker`)**:
    *   Calls **Placement Driver** (`PlacementService/GetWorker`) to get worker address and `shard_id`.
    *   Connects to **Worker Service**.
    *   Executes callback.
    *   **Callback**: Calls `translator.ToWorkerSearchRequest` (mapping `TopK` to `K`).
    *   **Worker Service** (`WorkerService/Search`) performs the search and returns `SearchResponse` (ids, scores).
    *   **Callback (after Worker response)**: Calls `translator.FromWorkerSearchResponse`.
        *   **CRITICAL FUNCTIONAL FLAW**: `translator.FromWorkerSearchResponse` sets `apigatewaypb.SearchResult.Payload` to `nil` and notes that the worker's `SearchResponse` doesn't include payloads. This means search results from the public API will **NEVER include metadata**.
5.  **API Gateway** returns `SearchResponse` (results with IDs, scores, but **empty payloads**).

**Identified Points:**
*   **Critical Functional Flaw:** `apigateway/internal/translator/translator.go` drops the `payload` during `Search` responses.
*   Worker's `SearchResponse` proto currently lacks a field for metadata.

## 7. Get User Profile (Client -> API Gateway -> Auth Service)

**Flow:**
1.  **Client** sends `GetUserProfileRequest` with `Authorization: Bearer <JWT>` to **API Gateway** (HTTP GET `/v1/user/profile`).
2.  **API Gateway (Auth Middleware)**: Validates session JWT, injects user info.
3.  **API Gateway** forwards request to **Auth Service** (gRPC `AuthService/GetUserProfile`).
4.  **Auth Service (JWT Middleware)**: Validates session JWT, injects `UserID` into context.
5.  **Auth Service (Handler)**:
    *   Retrieves `UserID` from context.
    *   Calls `etcdclient.Client.GetUserByID` (userID).
    *   **Auth Service (Etcd Client - `GetUserByID`)**:
        *   **PERFORMANCE BOTTLENECK**: Performs a full scan over all `vectron/users/` keys in etcd to find a user by ID.
    *   Returns `GetUserProfileResponse` (user info).
6.  **API Gateway** receives response and sends it back to **Client**.

**Identified Points:**
*   **Performance Bottleneck:** `etcdclient.Client.GetUserByID` is inefficient.

## 8. List API Keys (Client -> API Gateway -> Auth Service)

**Flow:**
1.  **Client** sends `ListAPIKeysRequest` with `Authorization: Bearer <JWT>` to **API Gateway** (HTTP GET `/v1/keys`).
2.  **API Gateway (Auth Middleware)**: Validates session JWT, injects user info.
3.  **API Gateway** forwards request to **Auth Service** (gRPC `AuthService/ListAPIKeys`).
4.  **Auth Service (JWT Middleware)**: Validates session JWT, injects `UserID` into context.
5.  **Auth Service (Handler)**:
    *   Retrieves `UserID` from context.
    *   Calls `etcdclient.Client.ListAPIKeys` (userID).
    *   **Auth Service (Etcd Client - `ListAPIKeys`)**:
        *   **PERFORMANCE BOTTLENECK**: Performs a full scan over all `vectron/apikeys/` keys in etcd to filter by user ID.
    *   Returns `ListAPIKeysResponse` (list of API keys).
6.  **API Gateway** receives response and sends it back to **Client**.

**Identified Points:**
*   **Performance Bottleneck:** `etcdclient.Client.ListAPIKeys` is inefficient.