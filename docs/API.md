# Vectron API Documentation

## Overview

Vectron provides a RESTful API for vector database operations. All API endpoints are accessible through the API Gateway service.

**Base URL:** `http://localhost:10012`

**Authentication:** All requests must include an `Authorization` header with a Bearer token:
```
Authorization: Bearer <sdk-jwt-token>
```

---

## Authentication

### Obtaining an SDK JWT Token

1. **Register/Login** via the web console at `http://localhost:10011`
2. **Create an API Key** in the dashboard
3. **Generate SDK JWT** using the API key:

```bash
curl -X POST http://localhost:10009/v1/sdk-jwt \
  -H "Authorization: Bearer <login-jwt>" \
  -H "Content-Type: application/json" \
  -d '{"api_key_id": "<key-prefix>"}'
```

---

## Collections API

### Create Collection

Creates a new vector collection with specified dimension and distance metric.

**Endpoint:** `POST /v1/collections`

**Request Body:**
```json
{
  "name": "my-collection",
  "dimension": 384,
  "distance": "cosine"
}
```

**Parameters:**
- `name` (string, required): Unique collection name
- `dimension` (int32, required): Vector dimension (e.g., 384, 768, 1536)
- `distance` (string): Distance metric - `"cosine"`, `"euclidean"`, or `"dot"`

**Response:**
```json
{
  "success": true
}
```

**Example:**
```bash
curl -X POST http://localhost:10012/v1/collections \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "products",
    "dimension": 384,
    "distance": "cosine"
  }'
```

---

### List Collections

Retrieves all collections in the system.

**Endpoint:** `GET /v1/collections`

**Response:**
```json
{
  "collections": ["products", "documents", "images"]
}
```

**Example:**
```bash
curl http://localhost:10012/v1/collections \
  -H "Authorization: Bearer <token>"
```

---

### Get Collection Status

Retrieves detailed status of a collection including shard information.

**Endpoint:** `GET /v1/collections/{name}/status`

**Response:**
```json
{
  "name": "products",
  "dimension": 384,
  "distance": "cosine",
  "shards": [
    {
      "shard_id": 1,
      "replicas": [1, 2, 3],
      "leader_id": 1,
      "ready": true
    }
  ]
}
```

**Example:**
```bash
curl http://localhost:10012/v1/collections/products/status \
  -H "Authorization: Bearer <token>"
```

---

## Points (Vectors) API

### Upsert Points

Inserts or updates vectors in a collection.

**Endpoint:** `POST /v1/collections/{collection}/points`

**Request Body:**
```json
{
  "points": [
    {
      "id": "item-001",
      "vector": [0.1, 0.2, 0.3, 0.4],
      "payload": {
        "name": "Product Name",
        "category": "electronics",
        "price": "99.99"
      }
    }
  ]
}
```

**Parameters:**
- `collection` (path, required): Collection name
- `points` (array): Array of point objects
  - `id` (string, required): Unique point identifier
  - `vector` (array of floats): Vector embedding
  - `payload` (object): Metadata key-value pairs

**Response:**
```json
{
  "upserted": 1
}
```

**Example:**
```bash
curl -X POST http://localhost:10012/v1/collections/products/points \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "points": [
      {
        "id": "product-001",
        "vector": [0.1, 0.2, 0.3, 0.4],
        "payload": {
          "name": "Wireless Headphones",
          "category": "electronics"
        }
      }
    ]
  }'
```

---

### Search Points

Performs k-nearest neighbors search on a collection.

**Endpoint:** `POST /v1/collections/{collection}/points/search`

**Request Body:**
```json
{
  "vector": [0.1, 0.2, 0.3, 0.4],
  "top_k": 10
}
```

**Parameters:**
- `collection` (path, required): Collection name
- `vector` (array of floats, required): Query vector
- `top_k` (uint32): Number of results to return (default: 10)

**Response:**
```json
{
  "results": [
    {
      "id": "item-001",
      "score": 0.95,
      "payload": {
        "name": "Product Name",
        "category": "electronics"
      }
    }
  ]
}
```

**Example:**
```bash
curl -X POST http://localhost:10012/v1/collections/products/points/search \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "vector": [0.12, 0.22, 0.32, 0.42],
    "top_k": 5
  }'
```

---

### Get Point by ID

Retrieves a specific point by its ID.

**Endpoint:** `GET /v1/collections/{collection}/points/{id}`

**Response:**
```json
{
  "point": {
    "id": "item-001",
    "vector": [0.1, 0.2, 0.3, 0.4],
    "payload": {
      "name": "Product Name"
    }
  }
}
```

**Example:**
```bash
curl http://localhost:10012/v1/collections/products/points/item-001 \
  -H "Authorization: Bearer <token>"
```

---

### Delete Point

Deletes a point from a collection.

**Endpoint:** `DELETE /v1/collections/{collection}/points/{id}`

**Response:**
```json
{}
```

**Example:**
```bash
curl -X DELETE http://localhost:10012/v1/collections/products/points/item-001 \
  -H "Authorization: Bearer <token>"
```

---

## Admin API

### System Health

Checks the overall system health status.

**Endpoint:** `GET /v1/system/health`

**Response:**
```json
{
  "status": "healthy",
  "services": {
    "placement_driver": "healthy",
    "workers": "healthy",
    "auth": "healthy"
  }
}
```

---

### Gateway Statistics

Retrieves API Gateway statistics and metrics.

**Endpoint:** `GET /v1/admin/stats`

**Response:**
```json
{
  "total_requests": 1523,
  "error_rate": 0.02,
  "avg_latency_ms": 45
}
```

---

### List Workers

Retrieves information about all worker nodes.

**Endpoint:** `GET /v1/admin/workers`

**Response:**
```json
{
  "workers": [
    {
      "worker_id": "1",
      "address": "127.0.0.1:10007",
      "status": "active"
    }
  ]
}
```

---

## Error Handling

### Error Response Format

```json
{
  "error": {
    "code": "NOT_FOUND",
    "message": "Collection not found"
  }
}
```

### Common Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `INVALID_ARGUMENT` | 400 | Invalid request parameters |
| `UNAUTHENTICATED` | 401 | Missing or invalid authentication |
| `PERMISSION_DENIED` | 403 | Insufficient permissions |
| `NOT_FOUND` | 404 | Resource not found |
| `ALREADY_EXISTS` | 409 | Resource already exists |
| `INTERNAL` | 500 | Internal server error |
| `UNAVAILABLE` | 503 | Service temporarily unavailable |

---

## Rate Limiting

API requests are rate-limited to ensure fair usage:

- **Default:** 100 requests per minute per API key
- **Search:** 1000 requests per minute per API key
- **Upsert:** 500 requests per minute per API key

Rate limit headers are included in responses:
```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1634567890
```

---

## Best Practices

### 1. Vector Dimension Consistency

Ensure all vectors in a collection have the same dimension as specified during creation.

### 2. Batch Operations

For better performance, batch multiple points in a single upsert request:
```json
{
  "points": [
    {"id": "1", "vector": [...]},
    {"id": "2", "vector": [...]},
    {"id": "3", "vector": [...]}
  ]
}
```

### 3. Metadata Usage

Use payloads to store searchable metadata:
- Category, tags, labels
- Timestamps
- User IDs
- Content metadata

### 4. Distance Metrics

Choose the appropriate distance metric for your use case:
- **Cosine:** Best for semantic similarity (text, embeddings)
- **Euclidean:** Best for geometric distance (images, coordinates)
- **Dot Product:** Best for normalized vectors

---

## Client Libraries

### Go

```go
import vectron "github.com/pavandhadge/vectron/clientlibs/go"

client, _ := vectron.NewClient("localhost:10010", "your-jwt-token")
defer client.Close()

client.CreateCollection("my-collection", 384, "cosine")
client.Upsert("my-collection", points)
results, _ := client.Search("my-collection", queryVector, 10)
```

### Python

```python
from vectron_client import VectronClient

client = VectronClient("localhost:10010", "your-api-key")
client.create_collection("my-collection", 384, "cosine")
client.upsert("my-collection", points)
results = client.search("my-collection", query_vector, top_k=10)
```

---

## Web Console

A web-based management console is available at:
**URL:** http://localhost:10011

Features:
- Collection management (create, list, delete)
- Vector operations (upsert, search, get, delete)
- API key management
- System monitoring and health
- User profile management

---

## Support

For issues and questions:
- GitHub Issues: https://github.com/pavandhadge/vectron/issues
- Documentation: https://docs.vectron.io
- API Status: https://status.vectron.io
