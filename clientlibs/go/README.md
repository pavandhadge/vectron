# Vectron Go Client Library

This directory contains the official Go client library for interacting with a Vectron cluster. It provides a simple and idiomatic Go interface that abstracts away the underlying gRPC communication and authentication details.

## Design and Architecture

The client library is designed to be a lightweight but robust wrapper around the public gRPC API exposed by the `apigateway` service.

- **gRPC Communication:** The client uses gRPC for all communication with the Vectron cluster, ensuring high performance and efficient data serialization via Protocol Buffers. A `*grpc.ClientConn` is established when `vectron.NewClient` is called and is reused for all subsequent requests.

- **Authentication:** When a new client is created, it stores the provided API key. For every RPC call, the library automatically injects the key into the outgoing request's metadata under the `authorization` header with the scheme `Bearer <apiKey>`. This is the mechanism used to authenticate with the `apigateway`.

- **Error Handling:** The library translates standard gRPC status codes into more idiomatic Go errors. For example, a `codes.NotFound` gRPC error is translated to the exported `vectron.ErrNotFound` variable. This allows consumers to use standard Go error handling patterns, like `errors.Is`, without needing to import gRPC-specific packages.

- **Type Abstraction:** The library defines its own simple, user-friendly data types (e.g., `vectron.Point`, `vectron.SearchResult`). It handles the conversion between these types and the more complex, generated Protocol Buffer types, providing a cleaner developer experience.

## Installation

```bash
go get github.com/pavandhadge/vectron/clientlibs/go
```

## Usage

### Creating a Client

First, create a new client instance, providing the address of the `apigateway` and your API key. The client maintains a persistent connection and is safe for concurrent use.

```go
import (
    "log"
    "github.com/pavandhadge/vectron/clientlibs/go"
)

func main() {
    // The address should point to the gRPC endpoint of the apigateway (e.g., "localhost:8081").
    client, err := vectron.NewClient("localhost:8081", "YOUR_API_KEY")
    if err != nil {
        log.Fatalf("Failed to create client: %v", err)
    }
    defer client.Close()

    // ... use the client
}
```

### Client Options and Help

The client exposes a help function and a configurable options struct for safety and performance.

```go
helpText, options := vectron.Help()
fmt.Println(helpText)
fmt.Printf("Defaults: %+v\n", options)
```

```go
opts := vectron.ClientOptions{
    UseTLS:            true,
    Timeout:           15 * time.Second,
    ExpectedVectorDim: 128,
    Compression:       "gzip",
    HedgedReads:       true,
    HedgeDelay:        75 * time.Millisecond,
}
client, err := vectron.NewClientWithOptions("my-host:8081", "YOUR_API_KEY", &opts)
```

Retries are enabled by default for read-only operations. To retry writes, set `RetryPolicy.RetryOnWrites = true`.

### Batch Upsert and Client Pooling

For high throughput, use client-side batching and optional pooling:

```go
batchOpts := &vectron.BatchOptions{BatchSize: 512, Concurrency: 4}
count, err := client.UpsertBatch("my-collection", points, batchOpts)
```

To cap batch payload size, set `MaxBatchBytes`:

```go
batchOpts := &vectron.BatchOptions{BatchSize: 512, MaxBatchBytes: 8 * 1024 * 1024}
```

```go
pool, err := vectron.NewClientPool("my-host:8081", "YOUR_API_KEY", &opts, 4)
if err != nil {
    log.Fatal(err)
}
defer pool.Close()
client = pool.Next()
```

### API Operations

All API operations return an `error` if the call fails.

#### Create a Collection

```go
err := client.CreateCollection("my-collection", 128, "cosine")
if err != nil {
    log.Printf("Failed to create collection: %v", err)
}
```

#### List Collections

```go
collections, err := client.ListCollections()
if err != nil {
    log.Printf("Failed to list collections: %v", err)
}
fmt.Println(collections)
```

#### Upsert Vectors

```go
points := []*vectron.Point{
    {
        ID:     "vec1",
        Vector: []float32{0.1, 0.2, 0.3, /* ... */},
    },
    {
        ID:     "vec2",
        Vector: []float32{0.4, 0.5, 0.6, /* ... */},
    },
}
upsertedCount, err := client.Upsert("my-collection", points)
if err != nil {
    log.Printf("Failed to upsert points: %v", err)
}
fmt.Printf("Upserted %d points\n", upsertedCount)
```

#### Search for Vectors

```go
queryVector := []float32{0.15, 0.25, 0.35, /* ... */}
results, err := client.Search("my-collection", queryVector, 5) // Get top 5 results
if err != nil {
    log.Printf("Failed to search: %v", err)
}
for _, res := range results {
    fmt.Printf("ID: %s, Score: %f\n", res.ID, res.Score)
}
```

### Advanced Error Handling

The library exports several error variables that can be used to check for specific failure modes:

- `vectron.ErrNotFound`
- `vectron.ErrInvalidArgument`
- `vectron.ErrAlreadyExists`
- `vectron.ErrAuthentication`
- `vectron.ErrInternalServer`

You can use `errors.Is` to check for these specific errors and handle them accordingly:

```go
import "errors"

_, err := client.Get("non-existent-collection", "some-id")
if errors.Is(err, vectron.ErrNotFound) {
    fmt.Println("The requested resource was not found. This is expected.")
} else if err != nil {
    log.Fatalf("An unexpected error occurred: %v", err)
}
```
