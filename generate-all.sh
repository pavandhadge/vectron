#!/bin/bash
set -e

echo "🚀 Generating protobuf code..."

# -------------------------------------------------
# PROTO INCLUDE PATHS (INPUT ONLY)
# -------------------------------------------------
PROTO_INCLUDE_PATHS="-I. \
-Ishared/proto \
-I$(go env GOPATH)/pkg/mod/github.com/googleapis/googleapis@v0.0.0-20251219214406-347b0e45a6ec \
-I$(go env GOPATH)/pkg/mod/github.com/grpc-ecosystem/grpc-gateway/v2@v2.27.3"

# -------------------------------------------------
# SERVICES
# -------------------------------------------------
SERVICES=("auth" "apigateway" "worker" "placementdriver" "reranker")

# -------------------------------------------------
# ALL PROTOS (RELATIVE TO shared/proto)
# -------------------------------------------------
ALL_PROTOS=(
  auth/auth.proto
  apigateway/apigateway.proto
  worker/worker.proto
  placementdriver/placementdriver.proto
  reranker/reranker.proto
)

# =================================================
# GO + GRPC + GRPC-GATEWAY — CENTRALIZED IN SHARED/PROTO
# =================================================
echo "🔧 Generating Go + gRPC + grpc-gateway code"

OUT_DIR="shared/proto"
echo "  → all services → $OUT_DIR"
mkdir -p "$OUT_DIR"

protoc ${PROTO_INCLUDE_PATHS} \
  --go_out="$OUT_DIR" --go_opt=paths=source_relative \
  --go-grpc_out="$OUT_DIR" --go-grpc_opt=paths=source_relative \
  --grpc-gateway_out="$OUT_DIR" --grpc-gateway_opt=paths=source_relative \
  "${ALL_PROTOS[@]}"

# =================================================
# GO CLIENT LIB
# =================================================
echo "📦 Generating Go client code"

OUT_DIR="clientlibs/go/proto"
echo "  → all services → $OUT_DIR"
mkdir -p "$OUT_DIR"

protoc ${PROTO_INCLUDE_PATHS} \
    --go_out="$OUT_DIR" --go_opt=paths=source_relative \
    --go-grpc_out="$OUT_DIR" --go-grpc_opt=paths=source_relative \
    "${ALL_PROTOS[@]}"

# =================================================
# OPENAPI — apigateway ONLY
# =================================================
echo "🌐 Generating OpenAPI spec (apigateway)"

mkdir -p apigateway/proto/openapi

protoc ${PROTO_INCLUDE_PATHS} \
  --openapiv2_out=apigateway/proto/openapi \
  --openapiv2_opt=logtostderr=true \
  apigateway/apigateway.proto

# =================================================
# JAVASCRIPT — COMMON CLIENT LIB
# =================================================
# echo "📦 Generating JavaScript client code"

# mkdir -p clientlibs/js/proto

# protoc ${PROTO_INCLUDE_PATHS} \
#   --js_out=import_style=commonjs,binary:clientlibs/js/proto \
#   --grpc-web_out=import_style=commonjs,mode=grpcwebtext:clientlibs/js/proto \
#   "${ALL_PROTOS[@]}"

# =================================================
# PYTHON — COMMON CLIENT LIB
# =================================================
# echo "🐍 Generating Python client code"

# mkdir -p clientlibs/python/vectron_client/proto

# python3 -m grpc_tools.protoc ${PROTO_INCLUDE_PATHS} \
#   --python_out=clientlibs/python/vectron_client/proto \
#   --grpc_python_out=clientlibs/python/vectron_client/proto \
#   "${ALL_PROTOS[@]}"

echo "✅ Protobuf generation complete."
