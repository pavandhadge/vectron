# Vectron Go Client Library

This directory contains the official Go client library for interacting with a Vectron cluster.

## Installation

```bash
go get github.com/pavandhadge/vectron/clientlibs/go
```

## Usage

### Creating a Client

First, create a new client instance, providing the address of the `apigateway` and your API key.

```go
import "github.com/pavandhadge/vectron/clientlibs/go"

func main() {
    // The address should point to the gRPC endpoint of the apigateway.
    client, err := vectron.NewClient("localhost:8081", "YOUR_API_KEY")
    if err != nil {
        log.Fatalf("Failed to create client: %v", err)
    }
    defer client.Close()

    // ... use the client
}
```

### API Operations

All API operations return an error if the call fails. The error can be inspected to determine the cause of the failure.

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
        Vector: []float32{0.1, 0.2, ...},
    },
    {
        ID:     "vec2",
        Vector: []float32{0.3, 0.4, ...},
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
queryVector := []float32{0.15, 0.25, ...}
results, err := client.Search("my-collection", queryVector, 5) // Get top 5 results
if err != nil {
    log.Printf("Failed to search: %v", err)
}
for _, res := range results {
    fmt.Printf("ID: %s, Score: %f\n", res.ID, res.Score)
}
```

### Error Handling

The library defines several error variables that can be used to check for specific failure modes:

*   `vectron.ErrNotFound`
*   `vectron.ErrInvalidArgument`
*   `vectron.ErrAlreadyExists`
*   `vectron.ErrAuthentication`
*   `vectron.ErrInternalServer`

You can use `errors.Is` to check for these specific errors:

```go
import "errors"

_, err := client.Get("non-existent-collection", "some-id")
if errors.Is(err, vectron.ErrNotFound) {
    fmt.Println("The requested resource was not found.")
}
```
