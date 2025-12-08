# Vectron Python Client

The official Python client for the Vectron vector database.

## Installation

```bash
pip install vectron-client
```

## Usage

```python
from vectron_client import VectronClient, Point

client = VectronClient(host="localhost:8080")

# Create a collection
client.create_collection(name="my-collection", dimension=128)

# List collections
collections = client.list_collections()
print(collections)

# Upsert points
points = [
    Point(id="1", vector=[0.1, 0.2, ...]),
    Point(id="2", vector=[0.3, 0.4, ...]),
]
client.upsert(collection="my-collection", points=points)

# Search for similar points
results = client.search(collection="my-collection", vector=[0.1, 0.2, ...], top_k=5)
print(results)
```
