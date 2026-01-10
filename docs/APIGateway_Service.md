# Vectron API Gateway Service Documentation

This document provides a detailed analysis of the Vectron `apigateway` service.

## 1. Overview

The `apigateway` is the public-facing entry point for the Vectron distributed vector database. It is a stateless service responsible for routing client requests to the appropriate backend services. It acts as a sophisticated facade, handling concerns like authentication, rate limiting, and request logging, while decoupling the public API from the internal system architecture.

### 1.1. Core Responsibilities

*   **Expose Public API:** Provides a dual interface with a high-performance gRPC API and a RESTful JSON API for broad compatibility.
*   **Authentication:** Validates client credentials (API Keys) by communicating with the `auth` service.
*   **Rate Limiting:** Enforces per-user rate limits on incoming requests.
*   **Request Logging:** Provides structured logs for all requests.
*   **Request Routing:** Acts as a smart reverse proxy, directing requests to the appropriate backend services.
*   **Service & Leader Discovery:** Dynamically finds the `placementdriver` leader and discovers which `worker` nodes are responsible for specific data.
*   **API Decoupling:** Translates data structures between the public-facing API and the internal service APIs, acting as an anti-corruption layer.

## 2. Architecture

The API Gateway is designed to be a lightweight, scalable, and secure entry point to the Vectron cluster.

### 2.1. Dual API: gRPC and REST

The gateway exposes its public API via two protocols to serve different client needs:

- **gRPC Server** (default `:8081`): For high-performance, low-latency communication from official client SDKs (Go, Python, JS).
- **HTTP/JSON Gateway** (default `:8080`): A RESTful interface that proxies requests to the gRPC server, allowing for easy interaction with standard tools like `curl`.

### 2.2. Request Routing Logic

The gateway is stateless and does not contain any vector data or collection metadata. Its primary role is to forward requests to the correct downstream service.

*   **Control Plane Operations** (`CreateCollection`, `ListCollections`, etc.): These are metadata-heavy operations. The gateway forwards these requests directly to the **`placementdriver`** service, which is the source of truth for cluster metadata.
*   **Data Plane Operations** (`Upsert`, `Search`, `Get`, `Delete`): These operations act on specific vectors. The gateway performs a multi-step process:
    1.  **Service Discovery:** It sends a `GetWorker` request to the `placementdriver` to find the address of the `worker` node and the specific `shard` ID responsible for the data.
    2.  **Connection:** It establishes a gRPC connection to the identified `worker` node.
    3.  **Request Forwarding:** It forwards the request to the `worker` for processing.

### 2.3. Leader Discovery for Placement Driver

The `placementdriver` is a fault-tolerant, Raft-based cluster, and only the leader node can handle write operations. The API Gateway implements a leader discovery mechanism:
- It maintains a list of all `placementdriver` node addresses.
- On startup, and whenever it detects a connection failure or a non-leader response, it iterates through the list, sending a test query until it successfully connects to the current leader.

### 2.4. Middleware Chain

All incoming gRPC requests are processed through a chain of interceptors:

- **Authentication (`AuthInterceptor`):** Enforces authentication by validating the client's API key. It calls the `auth` service's `ValidateAPIKey` RPC and injects the resulting claims (`UserID`, `Plan`, etc.) into the request context.
- **Logging (`LoggingInterceptor`):** Provides structured logging for every request, including method, user ID, status, and duration.
- **Rate Limiting (`RateLimitInterceptor`):** Enforces a per-user request rate limit using an in-memory, sliding-window algorithm. Note: This state is local to each gateway instance.

### 2.5. API Decoupling (Translator)

The gateway uses a `translator` package to act as an anti-corruption layer.
- The public API is defined by `apigateway.proto`.
- The internal service-to-service communication uses private protos like `worker.proto`.
- The `translator` contains functions to convert data structures between these two schemas. This allows the internal implementation of backend services to evolve independently of the public-facing API.

## 3. API Definition (from `shared/proto/apigateway/apigateway.proto`)

The public-facing gRPC and HTTP API for the `apigateway` is defined in the `apigateway.proto` file. This is the API that end-users interact with.

```protobuf
syntax = "proto3";

package vectron.v1;

import "google/api/annotations.proto";
import "google/api/field_behavior.proto";
import "protoc-gen-openapiv2/options/annotations.proto";

option go_package = "github.com/pavandhadge/vectron/shared/proto/apigateway";

// ===============================================
// Vectron Public API — The ONE users see
// ===============================================

service VectronService {
  // Create a new collection
  rpc CreateCollection(CreateCollectionRequest) returns (CreateCollectionResponse) {
    option (google.api.http) = {
      post: "/v1/collections"
      body: "*"
    };
  }

  // Upsert vectors
  rpc Upsert(UpsertRequest) returns (UpsertResponse) {
    option (google.api.http) = {
      post: "/v1/collections/{collection}/points"
      body: "*"
    };
  }

  // Search vectors
  rpc Search(SearchRequest) returns (SearchResponse) {
    option (google.api.http) = {
      post: "/v1/collections/{collection}/points/search"
      body: "*"
    };
  }

  // Get point by ID
  rpc Get(GetRequest) returns (GetResponse) {
    option (google.api.http) = {get: "/v1/collections/{collection}/points/{id}"};
  }

  // Delete point by ID
  rpc Delete(DeleteRequest) returns (DeleteResponse) {
    option (google.api.http) = {delete: "/v1/collections/{collection}/points/{id}"};
  }

  // List all collections
  rpc ListCollections(ListCollectionsRequest) returns (ListCollectionsResponse) {
    option (google.api.http) = {get: "/v1/collections"};
  }

  // Get collection status
  rpc GetCollectionStatus(GetCollectionStatusRequest) returns (GetCollectionStatusResponse) {
    option (google.api.http) = {get: "/v1/collections/{name}/status"};
  }
}

// ===============================================
// Messages
// ===============================================

message CreateCollectionRequest {
  string name = 1 [(google.api.field_behavior) = REQUIRED];
  int32 dimension = 2 [(google.api.field_behavior) = REQUIRED];
  string distance = 3; // "cosine", "euclidean", "dot"
}

message CreateCollectionResponse {
  bool success = 1;
}

message UpsertRequest {
  string collection = 1 [(google.api.field_behavior) = REQUIRED];
  repeated Point points = 2;
}

message UpsertResponse {
  int32 upserted = 1;
}

message SearchRequest {
  string collection = 1 [(google.api.field_behavior) = REQUIRED];
  repeated float vector = 2 [(google.api.field_behavior) = REQUIRED];

  // proto3 has no native defaults — this sets an OpenAPI (Swagger) default
  // so the generated OpenAPI spec will show top_k default = 10.
  uint32 top_k = 3 [(grpc.gateway.protoc_gen_openapiv2.options.openapiv2_field) = {default: "10"}];
}

message SearchResponse {
  repeated SearchResult results = 1;
}

message SearchResult {
  string id = 1;
  float score = 2;
  map<string, string> payload = 3;
}

message GetRequest {
  string collection = 1;
  string id = 2;
}

message GetResponse {
  Point point = 1;
}

message DeleteRequest {
  string collection = 1;
  string id = 2;
}

message DeleteResponse {}

message ListCollectionsRequest {}

message ListCollectionsResponse {
  repeated string collections = 1;
}

message GetCollectionStatusRequest {
  string name = 1;
}

message GetCollectionStatusResponse {
  string name = 1;
  int32 dimension = 2;
  string distance = 3;
  repeated ShardStatus shards = 4;
}

message ShardStatus {
  uint32 shard_id = 1;
  repeated uint64 replicas = 2;
  uint64 leader_id = 3;
  bool ready = 4;
}

message Point {
  string id = 1;
  repeated float vector = 2;
  map<string, string> payload = 3;
}
```


## 4. Service Entrypoint (`cmd/apigateway/main.go`)

The `main.go` file is the heart of the `apigateway` service. It contains the server implementation, the routing logic, and the initialization of all components.

### 4.1. `gatewayServer` Struct

This struct is the concrete implementation of the `VectronService` defined in the proto file.

```go
type gatewayServer struct {
	pb.UnimplementedVectronServiceServer
	pdAddrs  []string
	leader   *LeaderInfo
	leaderMu sync.RWMutex
}
```

*   `pdAddrs`: A slice of strings containing the addresses of all `placementdriver` nodes.
*   `leader`: A struct holding the gRPC client and connection to the current `placementdriver` leader.
*   `leaderMu`: A `sync.RWMutex` to ensure thread-safe access to the `leader` field, which is critical as multiple concurrent requests could trigger leader discovery.

### 4.2. Placement Driver Leader Discovery

The gateway is designed to be resilient to `placementdriver` leader changes. This is handled by the `getPlacementClient` and `updateLeader` methods.

*   `updateLeader`: This method is the core of the discovery logic. It iterates through all known `placementdriver` addresses (`pdAddrs`). For each address, it attempts to connect and sends a test RPC (`ListCollections`). The first node to respond successfully is considered the leader. It then closes any existing connection to a former leader and stores the new leader's connection details.
*   `getPlacementClient`: This is a thread-safe getter for the leader client. If a leader is already known, it returns the client. If not, it calls `updateLeader` to find one.

### 4.3. Worker Forwarding (`forwardToWorker`)

This powerful helper function encapsulates the entire service discovery and request forwarding logic for data plane operations.

```go
func (s *gatewayServer) forwardToWorker(ctx context.Context, collection string, vectorID string, call func(workerpb.WorkerServiceClient, uint64) (interface{}, error)) (interface{}, error)
```

1.  **Get Placement Client:** It first ensures it has a valid client for the `placementdriver` leader.
2.  **Discover Worker:** It calls the `placementdriver`'s `GetWorker` RPC, passing the collection name and vector ID. The `placementdriver` responds with the gRPC address of the correct `worker` node and the `shardID` where the data resides.
3.  **Connect to Worker:** It establishes a new gRPC connection to the worker at the discovered address.
4.  **Execute Callback:** It invokes the `call` function that was passed in, providing it with the `worker` client and the `shardID`.

This pattern elegantly separates the discovery/connection logic from the specific RPC logic, which is contained within the callback. It is used by `Upsert`, `Search`, `Get`, and `Delete`.

### 4.4. RPC Implementations

*   **Control Plane RPCs** (`CreateCollection`, `ListCollections`): These are metadata operations. The methods get a client for the `placementdriver` leader and forward the request directly. They also contain retry logic to handle cases where the leader might have changed between calls.
*   **Data Plane RPCs** (`Upsert`, `Search`, `Get`, `Delete`): These methods all use the `forwardToWorker` helper. They define a callback function that handles the specifics of their operation, such as translating the public request to the internal worker's request format using the `translator` package.

### 4.5. Service Initialization (`Start` and `main`)

*   The `Start` function orchestrates the entire startup process.
    1.  It initializes the `gatewayServer`.
    2.  It performs an initial `placementdriver` leader discovery with exponential backoff and retries to ensure a stable connection on startup.
    3.  It establishes a gRPC connection to the `auth` service.
    4.  **Middleware Chain:** It creates the main gRPC server, crucially using `grpc.ChainUnaryInterceptor` to apply the middleware in a specific order:
        1.  `middleware.AuthInterceptor`
        2.  `middleware.LoggingInterceptor`
        3.  `middleware.RateLimitInterceptor`
    5.  It registers the `gatewayServer` implementation.
    6.  It starts both the gRPC server and the HTTP/JSON gateway in separate goroutines.
*   The `main` function is simple: it calls `Start` and then blocks forever with `select {}` to keep the service running.


## 5. Middleware (`internal/middleware/`)

The `apigateway` uses a chain of gRPC Unary Interceptors to process incoming requests. These are applied in the `Start` function in `main.go` and execute in the following order:

1.  Authentication
2.  Logging
3.  Rate Limiting

### 5.1. `AuthInterceptor` (`auth.go`)

This is the first and most critical middleware in the chain.

*   **Purpose:** To authenticate incoming requests by validating the API key.
*   **Functionality:**
    1.  It is a higher-order function that receives an `authpb.AuthServiceClient` upon creation, which it uses to communicate with the `auth` service.
    2.  It extracts the API key from the `authorization` header of the incoming request. It is flexible enough to handle both `Bearer <key>` and raw `<key>` formats.
    3.  It makes a gRPC call to the `auth` service's `ValidateAPIKey` method. This delegation of validation is a key security pattern, keeping the `apigateway` stateless and centralizing auth logic in the `auth` service.
    4.  If the key is valid, the `auth` service returns the associated `UserID`, `Plan`, and `APIKeyID`.
    5.  The interceptor then injects these values into the request's `context` using `context.WithValue`. This makes the user's identity and plan available to all subsequent middleware and handlers in the chain.
    6.  If validation fails at any step, the request is rejected with an `Unauthenticated` error.

### 5.2. `LoggingInterceptor` (`logging.go`)

This middleware provides structured, request-level logging.

*   **Purpose:** To log the lifecycle of every gRPC request.
*   **Functionality:**
    1.  It records the time at the start of the request.
    2.  It retrieves the `UserID` (injected by the `AuthInterceptor`) and the client's IP address from the context.
    3.  It logs a `[gRPC][START]` line containing the full gRPC method name, user, and client IP.
    4.  It calls the next handler in the chain.
    5.  Once the handler returns, it calculates the total request `duration`.
    6.  It determines the final `status` (`OK` or `ERROR`) based on whether the handler returned an error.
    7.  It logs a `[gRPC][END]` line with the final status and duration.

### 5.3. `RateLimitInterceptor` (`ratelimit.go`)

This middleware provides per-user rate limiting.

*   **Purpose:** To protect the system from being overwhelmed by too many requests from a single user.
*   **Functionality:**
    1.  It is a higher-order function configured with a global `rps` (requests per second) limit.
    2.  It uses a global, in-memory `map` to track request counts for each `UserID`. Access to this map is protected by a `sync.Mutex`.
    3.  It implements a **fixed-window** algorithm. When a request from a user arrives:
        *   If the user is not in the map, or their one-second window has expired, a new limiter is created with a count of 1 and a reset time of one second in the future.
        *   If the user's window is still active, their count is simply incremented.
    4.  If a user's count exceeds the configured `rps`, the request is rejected with a `ResourceExhausted` error.
*   **Scalability Consideration:** The `README.md` and the implementation make it clear that this rate limiter's state is **local to each `apigateway` instance**. This is a significant architectural detail. If the gateway is deployed as a horizontally scaled cluster, each instance will have its own independent rate limiter. A user's effective rate limit would be `rps * number_of_gateway_instances`. A distributed rate limiter (e.g., using Redis or `etcd`) would be required for a shared global limit.


## 6. Translator (`internal/translator/translator.go`)

The `translator.go` file provides the concrete implementation of the "Anti-Corruption Layer" architectural pattern mentioned in the `README.md`. Its sole purpose is to decouple the public-facing API from the internal service-to-service APIs.

### 6.1. Purpose and Design

The translator contains a set of functions that convert data structures between the `apigatewaypb` (the public API contract) and the `workerpb` (the internal API contract used by worker nodes). This is a critical design choice that provides several benefits:

*   **Decoupling:** The public API can evolve independently of the internal APIs. For example, a field can be renamed in the `workerpb` without forcing a breaking change on public clients.
*   **Abstraction:** The public API can be designed for user-friendliness (e.g., using a `map[string]string` for payloads), while the internal API can be designed for performance (e.g., using `[]byte` for metadata).
*   **Facade:** It helps the gateway act as a true facade, hiding the complexity and implementation details of the backend services.

### 6.2. Function Naming Convention

The functions follow a clear and descriptive naming convention that indicates the direction of the translation:

*   `ToWorker...`: Translates data from the public `apigatewaypb` format to the internal `workerpb` format.
*   `FromWorker...`: Translates data from the internal `workerpb` format back to the public `apigatewaypb` format.

### 6.3. Implementation Details and Incompleteness

While the translator provides the necessary decoupling, analysis of the code reveals that some features are not fully implemented.

*   **Simple Field Mapping:** For most fields, the translation is a direct one-to-one mapping (e.g., `apigatewaypb.SearchRequest.TopK` is mapped to `workerpb.SearchRequest.K`).
*   **Incomplete Payload/Metadata Translation:** The code contains explicit `TODO` comments highlighting that the translation of vector payloads/metadata is incomplete.
    *   The public `Point` message uses a `map<string, string> payload`.
    *   The internal `Vector` message uses `bytes metadata`.
    *   The translation functions (`ToWorkerStoreVectorRequestFromPoint` and `FromWorkerGetVectorResponse`) currently set these fields to `nil`.
    *   The comments suggest that the intended implementation is to serialize the map to JSON before sending it to the worker and deserialize it upon retrieval.
    *   **As of this analysis, the gateway does not persist or retrieve vector payloads.**
*   **In-flight Data Transformation:** The translator is also responsible for transforming data as needed. For example, it converts the `uint32` `TopK` from the public API to the `int32` `K` expected by the worker.

The presence of these `TODO`s provides a clear roadmap for future development and highlights the current limitations of the data path.
