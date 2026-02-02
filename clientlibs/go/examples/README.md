# Vectron Go Client - Examples

This directory contains comprehensive examples demonstrating the Vectron vector database Go client library.

## Quick Start Example

```go
package main

import (
	"context"
	"fmt"
	"log"
	
	vectron "github.com/pavandhadge/vectron/clientlibs/go"
)

func main() {
	// Initialize client with API Gateway address and SDK JWT token
	client, err := vectron.NewClient("localhost:10010", "your-sdk-jwt-token")
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	
	fmt.Println("✅ Connected to Vectron!")
}
```

## Complete Examples

### 1. Basic Vector Operations (`basic_operations.go`)
Demonstrates:
- Creating collections
- Upserting single vectors
- Searching vectors
- Retrieving vectors by ID
- Deleting vectors

### 2. Batch Operations (`batch_operations.go`)
Demonstrates:
- Batch upsert for high-throughput scenarios
- Concurrent operations
- Error handling in batch operations

### 3. E-commerce Product Search (`ecommerce_search.go`)
Real-world example showing:
- Product catalog indexing
- Semantic search for products
- Metadata filtering (category, price range)
- Personalized recommendations

### 4. Document Search Engine (`document_search.go`)
Real-world example showing:
- Document embeddings storage
- Full-text + vector hybrid search
- Reranking results
- Search feedback collection

### 5. Recommendation System (`recommendation_system.go`)
Real-world example showing:
- User embedding profiles
- Item-to-item recommendations
- Collaborative filtering with vectors
- A/B testing different models

## Running Examples

1. Start Vectron services:
```bash
export JWT_SECRET=$(openssl rand -base64 32)
./run-all.sh
```

2. Get an SDK JWT token via the web console or API

3. Run an example:
```bash
cd clientlibs/go/examples
go run basic_operations.go
```

## Authentication

All examples require an SDK JWT token. To obtain one:

1. Register/Login via the web console at `http://localhost:10011`
2. Create an API key in the dashboard
3. Generate an SDK JWT using the API key

Or programmatically:
```bash
curl -X POST http://localhost:10009/v1/sdk-jwt \
  -H "Authorization: Bearer <login-jwt>" \
  -d '{"api_key_id": "<key-prefix>"}'
```
