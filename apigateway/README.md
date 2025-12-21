# API Gateway

The `apigateway` is the public-facing entry point for the Vectron distributed vector database. It is a stateless service responsible for handling client requests, authentication, and forwarding them to the appropriate backend services.

## Features

*   **gRPC and REST:** Exposes both a gRPC interface and a RESTful JSON API for all public operations.
*   **Authentication:** Secures the public endpoints using JWT-based authentication.
*   **Request Logging:** Logs incoming requests for monitoring and debugging.
*   **Rate Limiting:** Protects the system from overload with a token bucket rate limiter.
*   **Request Forwarding:** Intelligently forwards requests to the correct `worker` node by querying the `placementdriver` for shard and leader information.

## API

The public API is defined in `proto/apigateway/apigateway.proto`. It provides the following RPCs:

*   `CreateCollection`: Creates a new collection of vectors.
*   `Upsert`: Inserts or updates vectors in a collection.
*   `Search`: Performs a similarity search for vectors.
*   `Get`: Retrieves a vector by its ID.
*   `Delete`: Deletes a vector by its ID.
*   `ListCollections`: Lists all available collections.

## Building

To build the `apigateway` binary, run the following command from the root of the project:

```bash
make build-apigateway
```

The binary will be located at `bin/apigateway`.
