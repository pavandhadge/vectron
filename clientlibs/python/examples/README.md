# Vectron Python Client - Examples

This directory contains comprehensive examples demonstrating the Vectron vector database Python client library.

## Quick Start Example

```python
from vectron_client import VectronClient

# Initialize client with API Gateway address and API key
client = VectronClient(
    host="localhost:10010",
    api_key="your-api-key"
)

print("✅ Connected to Vectron!")
```

## Complete Examples

### 1. Basic Vector Operations (`basic_operations.py`)
Demonstrates:
- Creating collections
- Upserting single vectors
- Searching vectors
- Retrieving vectors by ID
- Deleting vectors

### 2. E-commerce Product Search (`ecommerce_search.py`)
Real-world example showing:
- Product catalog indexing with embeddings
- Semantic search for products
- Category-based filtering
- Price range queries with vector similarity

### 3. Semantic Document Search (`semantic_search.py`)
Real-world example showing:
- Document chunking and embedding
- Query-time semantic search
- Reranking top results
- Search analytics collection

### 4. Recommendation Engine (`recommendations.py`)
Real-world example showing:
- User embedding profiles
- Item-to-item recommendations
- Collaborative filtering
- Real-time personalization

## Running Examples

1. Install the Vectron client:
```bash
pip install vectron-client
```

2. Start Vectron services:
```bash
export JWT_SECRET=$(openssl rand -base64 32)
./run-all.sh
```

3. Get an API key via the web console or API

4. Run an example:
```bash
cd clientlibs/python/examples
python basic_operations.py
```

## Authentication

All examples require an API key. To obtain one:

1. Register/Login via the web console at `http://localhost:10011`
2. Navigate to API Keys section
3. Create a new API key
4. Copy the full key (shown only once)

## Requirements

- Python 3.8+
- `vectron-client` package
- `numpy` (for vector operations)
- `requests` (for HTTP API examples)

## API Reference

See [API Documentation](../../docs/API.md) for complete endpoint reference.
