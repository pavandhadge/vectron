#!/bin/bash
# This script generates the Go code from the protobuf definition.
# You must have protoc, protoc-gen-go, and protoc-gen-grpc-gateway installed.
#
# go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
# go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
# go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest

echo "Generating Go code from proto files..."

protoc -I . -I$(go env GOPATH)/pkg/mod/github.com/googleapis/googleapis@v0.0.0-20251219214406-347b0e45a6ec \
    --go_out . --go_opt paths=source_relative \
    --go-grpc_out . --go-grpc_opt paths=source_relative \
    --grpc-gateway_out . --grpc-gateway_opt paths=source_relative \
    proto/auth/auth.proto

echo "Done."
