# Vectron TypeScript/JavaScript Client Library

This directory contains the official TypeScript/JavaScript client library for interacting with a Vectron cluster. It provides a modern, `async/await`-based interface and can be used in Node.js environments.

## Design and Architecture

The client library is a wrapper around the public gRPC API exposed by the `apigateway` service, designed to provide an idiomatic developer experience in the JavaScript ecosystem.

- **gRPC Communication:** The client uses the `@grpc/grpc-js` library for all communication with the Vectron cluster. This ensures high-performance, strongly-typed, and efficient data transfer. A `VectronServiceClient` instance is created when `new VectronClient()` is called and is reused for subsequent requests.

- **Authentication:** When a new client is instantiated, it stores the provided API key. For every RPC call, the library automatically creates a gRPC `Metadata` object and attaches the `authorization` header with the scheme `Bearer <apiKey>`. This is the mechanism used to authenticate with the `apigateway`.

- **Error Handling:** The library abstracts away gRPC-specific error objects and provides a hierarchy of custom error classes. A gRPC `ServiceError` is translated into a more specific error (e.g., `NotFoundError`, `AuthenticationError`, all inheriting from a base `VectronError`). This allows consumers to use standard JavaScript error handling with `try...catch` blocks and `instanceof` checks.

- **Type Abstraction:** The library is written in TypeScript and exposes its own simple interfaces (e.g., `Point`, `SearchResult`). It handles the conversion between these clean interfaces and the generated Protocol Buffer message classes, offering full type safety and a simplified API surface.

## Installation

```bash
npm install vectron-client
```

_(Note: This package is not yet published to npm. The above is the intended installation method.)_

## Usage

### Creating a Client

Create a new client instance, providing the address of the `apigateway`'s gRPC endpoint and your API key.

```typescript
import { VectronClient } from "vectron-client";

// The host should point to the gRPC endpoint of the apigateway (e.g., "localhost:8081").
const client = new VectronClient("localhost:8081", "YOUR_API_KEY");

// ... use the client

// Close the connection when your application is shutting down.
client.close();
```

### Client Options and Help

The client exposes a help function and a typed options object for safety and performance.

```typescript
import { VectronClient } from "vectron-client";

const help = VectronClient.help();
console.log(help.text);
console.log(help.options);

const client = new VectronClient("my-host:8081", "YOUR_API_KEY", {
  useTLS: true,
  timeoutMs: 15000,
  expectedVectorDim: 128,
  compression: "gzip",
  hedgedReads: true,
  hedgeDelayMs: 75,
});
```

Retries are enabled by default for read-only operations. To retry writes, set `retryOnWrites: true`.

### Batch Upsert and Client Pooling

```typescript
const count = await client.upsertBatch("my-collection", points, 512, 4);
```

To cap batch payload size, set `maxBatchBytes` in options:

```typescript
const client = new VectronClient("my-host:8081", "YOUR_API_KEY", {
  maxBatchBytes: 8 * 1024 * 1024,
});
```

```typescript
import { VectronClientPool } from "vectron-client";

const pool = new VectronClientPool("my-host:8081", "YOUR_API_KEY", { useTLS: true }, 4);
const client = pool.next();
```

### API Operations

All API operations are `async` and return a `Promise`. They will throw an exception if the call fails.

```typescript
import { VectronClient, Point, SearchResult } from "vectron-client";
import { VectronError, NotFoundError } from "vectron-client/errors";

async function main() {
  const client = new VectronClient("localhost:8081", "YOUR_API_KEY");

  try {
    // Create a Collection
    await client.createCollection("my-ts-collection", 4, "cosine");
    console.log("Collection created.");

    // Upsert Vectors
    const pointsToUpsert: Point[] = [
      { id: "vec1", vector: [0.1, 0.2, 0.3, 0.4] },
      { id: "vec2", vector: [0.5, 0.6, 0.7, 0.8] },
    ];
    const upsertCount = await client.upsert("my-ts-collection", pointsToUpsert);
    console.log(`Upserted ${upsertCount} points.`);

    // Search for Vectors
    const results: SearchResult[] = await client.search(
      "my-ts-collection",
      [0.1, 0.2, 0.3, 0.4],
      1,
    );
    for (const res of results) {
      console.log(`Found: ID=${res.id}, Score=${res.score}`);
    }
  } catch (e) {
    if (e instanceof NotFoundError) {
      console.error(`A required resource was not found: ${e.message}`);
    } else if (e instanceof VectronError) {
      console.error(`A Vectron-specific error occurred: ${e.message}`);
    } else {
      console.error(`An unexpected error occurred: ${e}`);
    }
  } finally {
    client.close();
  }
}

main();
```

### Advanced Error Handling

The library exports a hierarchy of custom error classes. All library exceptions inherit from `VectronError`.

- `VectronError`: The base error class.
- `AuthenticationError`: For API key-related issues.
- `NotFoundError`: When a resource (collection or point) is not found.
- `InvalidArgumentError`: For invalid request parameters.
- `AlreadyExistsError`: When trying to create a resource that already exists.
- `InternalServerError`: For server-side errors.

You can use `instanceof` to check for specific error types and handle them accordingly:

```typescript
import { NotFoundError } from "vectron-client/errors";

try {
  const point = await client.get("my-collection", "non-existent-id");
} catch (e) {
  if (e instanceof NotFoundError) {
    console.log("Point not found, as expected.");
  } else {
    // Handle other errors
  }
}
```
