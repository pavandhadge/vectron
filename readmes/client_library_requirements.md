# Vectron Client Library: Requirements and API Specification

## 1. Introduction

This document outlines the requirements and API for client libraries of the Vectron distributed vector database. The goal is to provide a consistent, easy-to-use, and robust interface for interacting with the Vectron cluster in various programming languages.

The client library will abstract the complexities of communicating with the `apigateway`, handling authentication, and managing connections, allowing developers to focus on building their applications.

## 2. Core Requirements

### 2.1. Language Support

Official client libraries should be developed for the following languages:
- **Python** (Priority 1)
- **Go** (Priority 2)
- **JavaScript/TypeScript (Node.js)** (Priority 3)

### 2.2. Connection Management

- The client library must manage gRPC connections to the Vectron `apigateway`.
- It should support configuring the address of the `apigateway` and whether to use a secure connection (TLS).
- The client should be initialized with the `apigateway` address and an optional API key.
- The library should handle connection retries and backoff strategies for transient network errors.

### 2.3. Error Handling

- The library must provide clear and descriptive errors.
- gRPC status codes should be translated into idiomatic exceptions or error types for the target language.
- For example, a `NOT_FOUND` gRPC error should result in a `NotFoundException` or a similar error type.
- Validation errors (e.g., missing required fields) should be caught client-side when possible, before sending the request to the server.

### 2.4. Authentication

- The library must support API key-based authentication.
- The API key should be sent as a JWT in the `Authorization` header of each gRPC request.
- The library should provide a simple way to configure the API key during client initialization.

### 2.5. Ease of Use

- The API should be intuitive and well-documented.
- The library should be published to the standard package manager for the target language (e.g., PyPI for Python, npm for JavaScript).
- The use of native language features (e.g., async/await in Python and JavaScript) is highly encouraged.

## 3. API Reference

The client library will expose methods that correspond to the RPCs of the `VectronService`.

### 3.1. Client Initialization

**Method:** `VectronClient(host: str, api_key: str = None)`

- **Description:** Creates a new client instance.
- **Parameters:**
    - `host` (string, required): The address of the `apigateway` (e.g., `localhost:8080`).
    - `api_key` (string, optional): The API key for authentication.
- **Returns:** A new `VectronClient` instance.

---

### 3.2. Collection Management

**Method:** `create_collection(name: str, dimension: int, distance: str = 'euclidean')`

- **Description:** Creates a new collection.
- **Parameters:**
    - `name` (string, required): The name of the collection.
    - `dimension` (int, required): The dimension of the vectors in the collection.
    - `distance` (string, optional): The distance metric to use. Defaults to `'euclidean'`. Other possible values are `'cosine'` and `'dot'`.
- **Returns:** `None`.
- **Raises:** `CollectionAlreadyExistsError` if the collection already exists.

**Method:** `list_collections() -> List[str]`

- **Description:** Lists the names of all collections.
- **Returns:** A list of collection names.

---

### 3.3. Vector Operations

**Method:** `upsert(collection: str, points: List[Point])`

- **Description:** Inserts or updates vectors in a collection.
- **Parameters:**
    - `collection` (string, required): The name of the collection.
    - `points` (List[Point], required): A list of `Point` objects to upsert.
- **Returns:** The number of points upserted.

**Method:** `search(collection: str, vector: List[float], top_k: int = 10) -> List[SearchResult]`

- **Description:** Searches for the `k` nearest neighbors to a query vector.
- **Parameters:**
    - `collection` (string, required): The name of the collection.
    - `vector` (List[float], required): The query vector.
    - `top_k` (int, optional): The number of results to return. Defaults to 10.
- **Returns:** A list of `SearchResult` objects.

**Method:** `get(collection: str, point_id: str) -> Point`

- **Description:** Retrieves a point by its ID.
- **Parameters:**
    - `collection` (string, required): The name of the collection.
    - `point_id` (string, required): The ID of the point to retrieve.
- **Returns:** The `Point` object.
- **Raises:** `PointNotFoundError` if the point does not exist.

**Method:** `delete(collection: str, point_id: str)`

- **Description:** Deletes a point by its ID.
- **Parameters:**
    - `collection` (string, required): The name of the collection.
    - `point_id` (string, required): The ID of the point to delete.
- **Returns:** `None`.

## 4. Data Structures

The client library will expose the following data structures:

**`Point`**
- `id` (string): The unique ID of the point.
- `vector` (List[float]): The vector data.
- `payload` (Dict[str, any], optional): A dictionary of additional metadata.

**`SearchResult`**
- `id` (string): The ID of the matching point.
- `score` (float): The similarity score.
- `payload` (Dict[str, any], optional): The payload of the matching point.

## 5. Future Considerations

- **Batch operations:** For high-throughput applications, the library should support batching of `upsert` and `delete` operations.
- **Async API:** An asynchronous version of the API should be provided for languages that support it.
- **Metadata filtering:** The `search` method should be extended to support filtering based on the `payload` of the points.
