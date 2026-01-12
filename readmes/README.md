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

> here currently we were in middle of implementing updated auth setup

the plan is the suth will be totally based on the jwt

there will be 2 types of jwt 1 login 2 sdk

login one will only be usred for frontend

nad sdk one will we used by the sdks to do the stuff

and also a single user can have multiple api keys

sdk one comtain apikey, we have to have ai key generation, deletion setup etc

no user gets direct api key he gets the jwt token which has the api key inside along with other relevent stuff

u were middle of doing this i cut u off, so check hte progress and continue and finish it later we have to nodify the test namely @/home/pavan/programming/vectron/e2e_new_routes_test.go and helper @/home/pavan/programming/vectron/e2e_test_helpers_test.go

do not make any mistake follow the instruvtion to the word if anythign raise a question
