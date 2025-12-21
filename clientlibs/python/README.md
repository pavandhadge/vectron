# Vectron Python Client

The official Python client for the Vectron vector database.

## Installation

This client is not yet published to PyPI. To install it, you can install it directly from the git repository:

```bash
pip install git+https://github.com/pavandhadge/vectron.git#subdirectory=clientlibs/python
```

## Usage

### Creating a Client

First, create a new client instance, providing the address of the `apigateway` and your API key. The client can be used as a context manager.

```python
from vectron_client import VectronClient, Point, SearchResult
from vectron_client.exceptions import VectronError, NotFoundError

def main():
    # The host should point to the gRPC endpoint of the apigateway.
    with VectronClient(host="localhost:8081", api_key="YOUR_API_KEY") as client:
        try:
            # ... use the client
            client.create_collection(name="my-collection", dimension=4)
            print("Collection 'my-collection' created.")

            # Upsert points
            points_to_upsert = [
                Point(id="vec1", vector=[0.1, 0.2, 0.3, 0.4]),
                Point(id="vec2", vector=[0.5, 0.6, 0.7, 0.8]),
            ]
            upsert_count = client.upsert("my-collection", points_to_upsert)
            print(f"Upserted {upsert_count} points.")

            # Search
            results = client.search("my-collection", vector=[0.1, 0.2, 0.3, 0.4], top_k=1)
            for res in results:
                print(f"Found: ID={res.id}, Score={res.score}")

        except NotFoundError as e:
            print(f"Resource not found: {e}")
        except VectronError as e:
            print(f"An error occurred: {e}")

if __name__ == "__main__":
    main()
```

### Error Handling

The library uses a hierarchy of custom exceptions for error handling. All exceptions inherit from `VectronError`.

- `VectronError`: The base exception.
- `AuthenticationError`: For API key-related issues.
- `NotFoundError`: When a resource (collection or point) is not found.
- `InvalidArgumentError`: For invalid request parameters.
- `AlreadyExistsError`: When trying to create a resource that already exists.
- `InternalServerError`: For server-side errors.

You can catch specific exceptions to handle different failure modes:

```python
from vectron_client.exceptions import NotFoundError, InvalidArgumentError

try:
    client.get("my-collection", "non-existent-id")
except NotFoundError:
    print("Point not found, as expected.")
except InvalidArgumentError as e:
    print(f"Invalid argument: {e}")
```
