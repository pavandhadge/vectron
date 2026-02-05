# Vectron Client Libraries

This directory contains the official client libraries (SDKs) for interacting with the Vectron vector database. These libraries provide an idiomatic and convenient way to access the Vectron API from different programming languages.

## Philosophy

All client libraries share a common design philosophy:

- **Ease of Use:** Abstract away the complexities of gRPC communication and protocol buffers, providing a clean, language-idiomatic API.
- **Robust Error Handling:** Translate gRPC status codes into a clear hierarchy of custom errors or exceptions, making it easy for developers to handle failures.
- **Authentication:** Handle the API key authentication mechanism automatically. Developers simply provide their key at initialization, and the library manages adding the correct `Authorization` header to every request.
- **Type Safety:** Provide simple, user-friendly data types (like `Point` or `SearchResult`) and handle the conversion to and from the underlying protobuf message types.
- **Safety and Performance Defaults:** Ship with sensible defaults for timeouts, message size caps, and connection reuse. Each library includes a help function that describes configuration options.
- **Throughput Tools:** Each SDK includes optional batch upsert helpers and simple client pooling for high-QPS workloads.

---

## Supported Languages

Below are the currently supported languages. Each library is a wrapper around the public gRPC API exposed by the `apigateway` service.

### 1. [Go (`./go/`)](./go/README.md)

The Go client is a standard Go module that provides a simple, function-oriented interface. It uses exported error variables and `errors.Is` for error handling.

[**Click here for the detailed Go Client README.**](./go/README.md)

### 2. [TypeScript / JavaScript (`./js/`)](./js/README.md)

The JS client is a TypeScript-first library that can be used in Node.js environments. It provides a modern, `async/await`-based API and uses a hierarchy of custom error classes for exception handling.

[**Click here for the detailed JS Client README.**](./js/README.md)

### 3. [Python (`./python/`)](./python/README.md)

The Python client provides a clean, object-oriented interface. It makes extensive use of dataclasses for models and can be used as a context manager to ensure proper resource cleanup. It uses a hierarchy of custom exceptions for error handling.

[**Click here for the detailed Python Client README.**](./python/README.md)
