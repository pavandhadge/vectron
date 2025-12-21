# Vectron TypeScript/JavaScript Client Library

This directory contains the official TypeScript/JavaScript client library for interacting with a Vectron cluster. It can be used in both Node.js and browser environments (with a gRPC-web proxy).

## Installation

```bash
npm install vectron-client
```
*(Note: This package is not yet published to npm. The above is the intended installation method.)*

## Usage

### Creating a Client

First, create a new client instance, providing the address of the `apigateway` and your API key.

```typescript
import { VectronClient, Point, SearchResult } from 'vectron-client';
import { VectronError, NotFoundError } from 'vectron-client/errors';

async function main() {
    // The host should point to the gRPC endpoint of the apigateway.
    const client = new VectronClient("localhost:8081", "YOUR_API_KEY");

    try {
        // ... use the client
        await client.createCollection("my-ts-collection", 4);
        console.log("Collection 'my-ts-collection' created.");

        // Upsert points
        const pointsToUpsert: Point[] = [
            { id: "vec1", vector: [0.1, 0.2, 0.3, 0.4] },
            { id: "vec2", vector: [0.5, 0.6, 0.7, 0.8] },
        ];
        const upsertCount = await client.upsert("my-ts-collection", pointsToUpsert);
        console.log(`Upserted ${upsertCount} points.`);

        // Search
        const results = await client.search("my-ts-collection", [0.1, 0.2, 0.3, 0.4], 1);
        for (const res of results) {
            console.log(`Found: ID=${res.id}, Score=${res.score}`);
        }

    } catch (e) {
        if (e instanceof NotFoundError) {
            console.error(`Resource not found: ${e.message}`);
        } else if (e instanceof VectronError) {
            console.error(`A Vectron error occurred: ${e.message}`);
        } else {
            console.error(`An unexpected error occurred: ${e}`);
        }
    } finally {
        client.close();
    }
}

main();
```

### Error Handling

The library uses a hierarchy of custom error classes for error handling. All exceptions inherit from `VectronError`.

- `VectronError`: The base error class.
- `AuthenticationError`: For API key-related issues.
- `NotFoundError`: When a resource (collection or point) is not found.
- `InvalidArgumentError`: For invalid request parameters.
- `AlreadyExistsError`: When trying to create a resource that already exists.
- `InternalServerError`: For server-side errors.

You can use `instanceof` to check for specific error types:

```typescript
import { NotFoundError } from 'vectron-client/errors';

try {
    await client.get("my-collection", "non-existent-id");
} catch (e) {
    if (e instanceof NotFoundError) {
        console.log("Point not found, as expected.");
    }
}
```
