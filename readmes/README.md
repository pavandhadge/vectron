# Vectron

Vectron is a distributed vector database designed for high-performance similarity search. It is built with a microservices architecture in Go and provides client libraries for Go, Python, and JavaScript.

## Architecture

Vectron consists of three core services:

- **`apigateway`**: The public-facing gateway that handles client requests (both gRPC and HTTP/JSON). It authenticates requests, forwards them to the appropriate worker nodes, and aggregates the results. It discovers worker nodes by querying the `placementdriver`.

- **`placementdriver`**: The brain of the cluster. It is a distributed, fault-tolerant service that manages the cluster state, including worker node health, collection metadata, and data sharding information. It uses the Raft consensus algorithm to ensure consistency and availability.

- **`worker`**: The workhorse of the cluster. It is responsible for storing, indexing, and searching vectors. Each worker manages a subset of the data (shards) and performs the actual similarity search. (Further details to be added after a more in-depth analysis of the `worker` service).

## Client Libraries

Vectron provides client libraries for the following languages:

- **Go**: `clientlibs/go`
- **Python**: `clientlibs/python`
- **JavaScript**: `clientlibs/js`

These libraries provide a simple and idiomatic way to interact with the Vectron API.

## Getting Started

### Prerequisites

- Go 1.19+

### Building

To build all the services, run the following command:

```bash
make build
```

This will create the binaries for `apigateway`, `placementdriver`, and `worker` in the `bin` directory.

### Running

(Instructions on how to run the services will be added here once the configuration and startup procedures are fully understood).
