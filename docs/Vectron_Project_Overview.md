# Vectron Project Overview

This document provides a high-level overview of the Vectron project, its architecture, and its components.

## 1. Architecture

Vectron is a distributed system built on a microservices architecture. The backend services are written in Go and communicate with each other using gRPC. The system is designed to be a vector database, with services for managing authentication, handling API requests, distributing data, and performing vector searches.

The key architectural patterns are:

*   **Microservices:** The project is divided into several independent services, each with a specific responsibility. This promotes modularity, scalability, and independent development.
*   **gRPC for Inter-Service Communication:** All backend services communicate with each other using gRPC, a high-performance RPC framework. The API definitions are specified in `.proto` files located in the `shared/proto` directory.
*   **API Gateway:** The `apigateway` service acts as an entry point for external clients. It exposes a RESTful JSON API and translates incoming requests into gRPC calls to the appropriate backend services. This provides a single, consistent interface for clients and decouples them from the internal microservice architecture.
*   **Multi-Module Go Workspace:** The project is organized as a Go workspace, which allows for the simultaneous development and management of multiple Go modules within a single repository.

## 2. Components

The project is composed of the following main components:

| Component         | Location                | Description                                                                                                                              |
| ----------------- | ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| **Authentication Service** | `auth/service`          | Manages user authentication and authorization. It likely handles API key generation and validation.                                    |
| **API Gateway**   | `apigateway`            | Exposes a public-facing RESTful API, handles request routing, and communicates with other backend services via gRPC.                       |
| **Worker**        | `worker`                | The core data processing and storage engine. It likely manages vector embeddings, performs similarity searches, and stores data in shards. |
| **Placement Driver** | `placementdriver`       | A coordination service that manages the distribution of data and workload across the `worker` nodes. It likely uses Raft for consensus. |
| **Shared Code**   | `shared`                | Contains common code used by multiple services, most importantly the protobuf definitions for the gRPC APIs.                            |
| **Client Libraries** | `clientlibs`            | Generated client libraries for JavaScript and Python that allow easy interaction with the Vectron API.                                   |

## 3. Build Process

The project uses a combination of a `Makefile` and a shell script (`generate-all.sh`) to manage the build process.

*   **`generate-all.sh`:** This script is responsible for generating Go, JavaScript, and Python code from the `.proto` files. It uses `protoc` with various plugins to create gRPC server and client code, as well as the gRPC-gateway for the RESTful API.
*   **`Makefile`:** This file provides simple targets for building the individual Go services and a main `build` target to compile all services. The compiled binaries are placed in the `bin` directory.

## 4. How the System Works Together (Initial Hypothesis)

1.  A client makes a request to the RESTful API exposed by the `apigateway`.
2.  The `apigateway` authenticates the request, possibly by communicating with the `auth` service.
3.  The `apigateway` translates the RESTful request into a gRPC call and forwards it to the appropriate service, likely the `placementdriver` or a `worker`.
4.  The `placementdriver` determines which `worker`(s) should handle the request and forwards it accordingly.
5.  The `worker`(s) process the request (e.g., indexing data, performing a search) and return a response.
6.  The response is propagated back through the chain to the client.

This is a preliminary overview based on the project structure. The detailed documentation for each component will provide a more in-depth understanding of the system.
