#!/bin/bash
# This script generates Go code from all protobuf definitions in the project.
# It should be run from the project root.

# Ensure protoc, protoc-gen-go, protoc-gen-go-grpc, and protoc-gen-grpc-gateway are installed.
# go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
# go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
# go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest
# go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@latest # For OpenAPI annotations

echo "Generating Go code from proto files..."

PROTO_INCLUDE_PATHS="-I. -Ishared/proto -I$(go env GOPATH)/pkg/mod/github.com/googleapis/googleapis@v0.0.0-20251219214406-347b0e45a6ec -I$(go env GOPATH)/pkg/mod/github.com/grpc-ecosystem/grpc-gateway/v2@v2.27.3"

# Generate code for shared protos
protoc ${PROTO_INCLUDE_PATHS} \
    --go_out . --go_opt paths=source_relative \
    --go-grpc_out . --go-grpc_opt paths=source_relative \
    --grpc-gateway_out . --grpc-gateway_opt paths=source_relative \
    shared/proto/auth/auth.proto \
    shared/proto/placementdriver/placementdriver.proto \
    shared/proto/worker/worker.proto

# Generate code for apigateway's own proto
mkdir -p ./shared/proto/apigateway/openapi # Ensure the output directory exists

protoc ${PROTO_INCLUDE_PATHS} \
  --go_out . --go_opt paths=source_relative \
  --go-grpc_out . --go-grpc_opt paths=source_relative \
  --grpc-gateway_out . --grpc-gateway_opt paths=source_relative \
  --grpc-gateway_opt logtostderr=true \
  --openapiv2_out ./shared/proto/apigateway/openapi \
  --openapiv2_opt logtostderr=true \
  shared/proto/apigateway/apigateway.proto

# Ensure protoc-gen-js and protoc-gen-python are installed.
# To install protoc-gen-js (Node.js): npm install -g google-protobuf grpc-tools
# To install grpc-web (for grpc-web_out): npm install -g grpc-web-tools
# To install protoc-gen-python and grpc_python_out: pip install grpcio-tools

echo "Generating JavaScript code from proto files..."
# Generate JS for apigateway (client-facing)
protoc ${PROTO_INCLUDE_PATHS} \
    --js_out=import_style=commonjs,binary:clientlibs/js/proto/apigateway \
    --grpc-web_out=import_style=commonjs,mode=grpcwebtext:clientlibs/js/proto/apigateway \
    shared/proto/apigateway/apigateway.proto

echo "Generating Python code from proto files..."
# Generate Python for all shared protos (used by all client libs)
python3 -m grpc_tools.protoc ${PROTO_INCLUDE_PATHS} \
    --python_out=. \
    --grpc_python_out=. \
    shared/proto/auth/auth.proto \
    shared/proto/apigateway/apigateway.proto \
    shared/proto/placementdriver/placementdriver.proto \
    shared/proto/worker/worker.proto

echo "Done."
