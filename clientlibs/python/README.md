# Vectron Python Client

This directory contains the official Python client for the Vectron vector database. It provides a robust, idiomatic Python interface for all API operations, abstracting the underlying gRPC communication.

## Design and Architecture

The client library is a wrapper around the public gRPC API exposed by the `apigateway` service, designed to provide a clean and Pythonic developer experience.

- **gRPC Communication:** The client uses the standard `grpcio` library for all communication with the Vectron cluster. It creates a `VectronServiceStub` when `VectronClient` is instantiated, and this stub is used for all subsequent RPCs.

- **Authentication:** When a new client is created, it stores the provided API key. For every RPC call, the library automatically creates a metadata tuple `('authorization', 'Bearer <apiKey>')` and attaches it to the outgoing request. This is the mechanism used to authenticate with the `apigateway`.

- **Error Handling:** The library provides a hierarchy of custom exceptions (e.g., `NotFoundError`, `AuthenticationError`, all inheriting from `VectronError`). It wraps every gRPC call in a `try...except` block and translates `grpc.RpcError` exceptions into more specific, user-friendly custom exceptions. This allows consumers to use standard Python `try...except` blocks without needing to handle gRPC-specific status codes.

- **Type Abstraction:** The library uses Pythonic data structures, like dataclasses (`Point`, `SearchResult`) and standard lists/dicts, for its public interface. It handles the conversion between these types and the generated Protocol Buffer message classes, offering a fully typed and intuitive API.

- **Resource Management:** The client is designed as a context manager. Using it in a `with` statement ensures that the underlying gRPC channel is automatically closed when the block is exited, preventing resource leaks.

## Installation

This client is not yet published to PyPI. To install it, you can install it directly from the git repository:

```bash
pip install "git+https://github.com/pavandhadge/vectron.git#subdirectory=clientlibs/python"
```

## Usage

### Creating a Client

Create a new client instance, providing the address of the `apigateway`'s gRPC endpoint and your API key. Using the client as a context manager is the recommended approach.

```python
from vectron_client import VectronClient

# The host should point to the gRPC endpoint of the apigateway (e.g., "localhost:8081").
with VectronClient(host="localhost:8081", api_key="YOUR_API_KEY") as client:
    # ... use the client
    pass
```

### API Operations

All API methods will raise a `VectronError`-derived exception if the call fails.

```python
from vectron_client import VectronClient, Point
from vectron_client.exceptions import VectronError, NotFoundError

def main():
    with VectronClient(host="localhost:8081", api_key="YOUR_API_KEY") as client:
        try:
            # Create a Collection
            client.create_collection(name="my-py-collection", dimension=4, distance="cosine")
            print("Collection created.")

            # List Collections
            collections = client.list_collections()
            print(f"Available collections: {collections}")

            # Upsert Vectors
            points_to_upsert = [
                Point(id="vec1", vector=[0.1, 0.2, 0.3, 0.4]),
                Point(id="vec2", vector=[0.5, 0.6, 0.7, 0.8]),
            ]
            upsert_count = client.upsert("my-py-collection", points_to_upsert)
            print(f"Upserted {upsert_count} points.")

            # Search for Vectors
            results = client.search("my-py-collection", vector=[0.1, 0.2, 0.3, 0.4], top_k=1)
            for res in results:
                print(f"Found: ID={res.id}, Score={res.score}")

        except NotFoundError as e:
            print(f"A required resource was not found: {e}")
        except VectronError as e:
            print(f"A Vectron-specific error occurred: {e}")

if __name__ == "__main__":
    main()
```

### Advanced Error Handling

The library provides a hierarchy of custom exceptions defined in the `vectron_client.exceptions` module. All library exceptions inherit from `VectronError`.

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
    point = client.get("my-collection", "non-existent-id")
except NotFoundError:
    print("Point not found, as expected.")
except InvalidArgumentError as e:
    print(f"Invalid argument provided: {e}")
```
