# Vectron API Gateway

The `apigateway` is the public-facing entry point for the Vectron distributed vector database. It is a stateless service responsible for routing client requests to the appropriate backend services. It acts as a sophisticated facade, handling concerns like authentication, rate limiting, and request logging, while decoupling the public API from the internal system architecture.

## Architecture and Design

The API Gateway is designed to be a lightweight, scalable, and secure entry point to the Vectron cluster.

### 1. Dual API: gRPC and REST

The gateway exposes its public API via two protocols to serve different client needs:

- **gRPC Server** (default `:8081`): For high-performance, low-latency communication from official client SDKs (Go, Python, JS). It uses protocol buffers for efficient serialization.
- **HTTP/JSON Gateway** (default `:8080`): A RESTful interface that proxies requests to the gRPC server. This is generated using `grpc-gateway` and allows for easy interaction with standard tools like `curl` or web applications.

### 2. Request Routing and Service Discovery

The gateway is stateless and does not contain any vector data or collection metadata itself. Its primary role is to forward requests to the correct downstream service.

- **Control Plane Operations** (`CreateCollection`, `ListCollections`, etc.): These are metadata-heavy operations. The gateway forwards these requests directly to the **Placement Driver (`placementdriver`)** service, which is the source of truth for cluster metadata.
- **Data Plane Operations** (`Upsert`, `Search`, `Get`, `Delete`): These operations act on specific vectors. The gateway performs a multi-step process:
  1.  **Service Discovery:** It sends a `GetWorker` request to the `placementdriver` to find the address of the `worker` node and the specific `shard` ID responsible for the given collection and vector ID.
  2.  **Connection:** It establishes a gRPC connection to the identified `worker` node.
  3.  **Request Forwarding:** It forwards the request to the `worker` for processing.

### 3. Leader Discovery for Placement Driver

The `placementdriver` is a fault-tolerant, Raft-based cluster. Only one node (the leader) can handle write operations or authoritative metadata reads. The API Gateway implements a leader discovery mechanism:

- It maintains a list of all `placementdriver` node addresses.
- On startup, and whenever it detects a connection failure or a non-leader response, it iterates through the list, sending a test query (`ListCollections`) until it successfully connects to the current leader.
- This makes the gateway resilient to `placementdriver` leader changes.

### 4. Middleware Chain

All incoming gRPC requests are processed through a chain of interceptors that handle cross-cutting concerns:

- **Authentication (`AuthInterceptor`):**
  - Enforces JWT-based authentication.
  - Extracts the `Authorization: Bearer <token>` header.
  - Parses and validates the JWT, checking its signature and expiration.
  - Extracts custom claims (`UserID`, `Plan`, `APIKeyID`) from the token and injects them into the request context for use by other middleware and handlers.
- **Logging (`LoggingInterceptor`):**
  - Provides structured logging for every request.
  - Logs the start and end of a request, including the gRPC method, user ID, client IP address, final status (`OK`/`ERROR`), and total processing duration.
- **Rate Limiting (`RateLimitInterceptor`):**
  - Enforces a request rate limit on a per-user basis.
  - It uses an in-memory, sliding-window algorithm to track requests per second for each `UserID`.
  - If a user exceeds the configured limit, the request is rejected with a `ResourceExhausted` error.
  - **Note:** The rate-limiting state is local to each gateway instance and is not shared across a cluster of gateways.

### 5. API Decoupling (Translator)

The gateway uses a `translator` package to act as an anti-corruption layer.

- The public API is defined by the `apigateway.proto` file.
- The internal service-to-service communication with workers uses a private `worker.proto` file.
- The `translator` contains functions to convert data structures between these two schemas. This is a key architectural choice that allows the internal implementation of workers to evolve independently of the public-facing API, preventing breaking changes for clients.

## Public API

The public API is formally defined in `proto/apigateway/apigateway.proto`. It provides the following RPCs:

- `CreateCollection`: Creates a new collection of vectors.
- `Upsert`: Inserts or updates vectors in a collection.
- `Search`: Performs a similarity search for vectors.
- `Get`: Retrieves a vector by its ID.
- `Delete`: Deletes a vector by its ID.
- `ListCollections`: Lists all available collections.
- `GetCollectionStatus`: Retrieves detailed status and shard information for a collection.

## Configuration

The service is configured using environment variables:

| Variable           | Description                                              | Default                   |
| ------------------ | -------------------------------------------------------- | ------------------------- |
| `GRPC_ADDR`        | Address for the gRPC server to listen on.                | `:8081`                   |
| `HTTP_ADDR`        | Address for the HTTP/JSON gateway to listen on.          | `:8080`                   |
| `PLACEMENT_DRIVER` | Comma-separated list of placement driver node addresses. | `placement:6300`          |
| `JWT_SECRET`       | Secret key for signing and verifying JWT tokens.         | `CHANGE_ME_IN_PRODUCTION` |
| `RateLimitRPS`     | Global requests-per-second limit for the rate limiter.   | `100`                     |

## Building and Running

To build the `apigateway` binary, run the following command from the root of the project:

```bash
make build-apigateway
```

The binary will be located at `bin/apigateway`. You can run it directly, ensuring the required environment variables are set.
