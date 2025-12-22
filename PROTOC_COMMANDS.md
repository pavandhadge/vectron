# Protocol Buffer Compilation Commands

This file contains the `protoc` commands to compile the protocol buffer files for each service and client library.

## Prerequisites

You must have the following tools installed:

- `protoc`: The protocol buffer compiler.
- Go plugins:
  - `protoc-gen-go`: `go install google.golang.org/protobuf/cmd/protoc-gen-go`
  - `protoc-gen-go-grpc`: `go install google.golang.org/grpc/cmd/protoc-gen-go-grpc`
  - `protoc-gen-grpc-gateway`: `go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway`
- Python tools:
  - `grpcio-tools`: `pip install grpcio-tools`
- JavaScript/TypeScript tools:
  - `grpc-tools`: `npm install -g grpc-tools`
  - `ts-proto`: `npm install -g ts-proto`

You will also need the Google APIs repository for the `google/api/annotations.proto` imports. It is recommended to clone it to a well-known directory.

```bash
go get  https://github.com/googleapis/googleapis.git
```

All commands should be run from the root of the `vectron` repository.

## Go

These commands will generate the Go gRPC code (`.pb.go`), gRPC gateway code (`.pb.gw.go`), and OpenAPI specifications (`.swagger.json`).

```bash
# Define the Google APIs path
GOOGLE_APIS_DIR=/path/to/googleapis

# --- API Gateway ---
#
 protoc   -I .   -I $(go env GOMODCACHE)/github.com/googleapis/googleapis@v0.0.0-20251219214406-347b0e45a6ec   -I $(go env GOMODCACHE)/github.com/grpc-ecosystem/grpc-gateway/v2@v2.27.3   --go_out=.  --go_opt=paths=source_relative   --go-grpc_out=. --go-grpc_opt=paths=source_relative   --grpc-gateway_out=. --grpc-gateway_opt=paths=source_relative   --openapiv2_out=openapi   apigateway.proto

# --- Placement Driver ---
protoc -I. -I${GOOGLE_APIS_DIR} \
    --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    placementdriver.proto

# --- Worker ---
protoc -I. -I${GOOGLE_APIS_DIR} \
    --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    worker.proto
```

## Python

This command generates the Python gRPC stubs for the client library.

```bash
# Define the Google APIs path
GOOGLE_APIS_DIR=/path/to/googleapis

python -m grpc_tools.protoc \
    -I. \
    -I${GOOGLE_APIS_DIR} \
    --python_out=. \
    --grpc_python_out=. \
    apigateway.proto
```

## JavaScript / TypeScript

This command generates the TypeScript client code using `ts-proto`.

```bash
# Define the Google APIs path
GOOGLE_APIS_DIR=/path/to/googleapis

protoc \
    --plugin="protoc-gen-ts_proto=$(npm root)/.bin/protoc-gen-ts_proto" \
    --ts_proto_out=. \
    --ts_proto_opt=outputServices=grpc-js,env=node \
    -I. \
    -I${GOOGLE_APIS_DIR} \
    apigateway.proto
```
