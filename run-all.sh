#!/bin/bash

# A script to run all Vectron services, including the frontend, for local development.

# --- Cleanup Function ---
# This function will be called when the script exits.
cleanup() {
    echo " "
    echo "🛑 Stopping all services..."
    # Kill all background jobs started by this script
    kill $(jobs -p) 2>/dev/null
    # Stop the etcd container
    docker stop etcd > /dev/null
    # Clean up temporary data directories
    rm -rf /tmp/pd_dev_*
    rm -rf /tmp/worker_dev_*
    echo "✅ Cleanup complete."
}

# Trap the EXIT signal to call the cleanup function
trap cleanup EXIT

# --- Configuration ---
echo "🚀 Starting all Vectron services..."

# Ports
export PD_GRPC_1=10001
export PD_RAFT_1=10002
export PD_GRPC_2=10003
export PD_RAFT_2=10004
export PD_GRPC_3=10005
export PD_RAFT_3=10006
export WORKER_GRPC=10007
export AUTH_GRPC=10008
export AUTH_HTTP=10009
export APIGATEWAY_GRPC=10010
export FRONTEND_PORT=10011

# Endpoints
export ETCD_ENDPOINTS="127.0.0.1:2379"
export PD_ADDRS="127.0.0.1:${PD_GRPC_1},127.0.0.1:${PD_GRPC_2},127.0.0.1:${PD_GRPC_3}"
export AUTH_SERVICE_ADDR="127.0.0.1:${AUTH_GRPC}"

# Secrets
export JWT_SECRET="dev-secret-key-that-is-not-so-secret"

# --- Build Binaries ---
echo "🔧 Building service binaries..."
make build > /dev/null
if [ $? -ne 0 ]; then
    echo "❌ Failed to build binaries. Please check for compilation errors."
    exit 1
fi
echo "✅ Binaries built successfully."

# --- Start Services ---

# 1. Start Etcd
echo "▶️  Starting etcd from Docker..."
docker start etcd > /dev/null
if [ $? -ne 0 ]; then
    echo "⚠️  Could not start 'etcd' container. Please ensure you have an etcd container created."
    echo "   You can create one with: docker run -d -p 2379:2379 --name etcd quay.io/coreos/etcd:v3.5.0 /usr/local/bin/etcd --advertise-client-urls http://0.0.0.0:2379 --listen-client-urls http://0.0.0.0:2379"
    exit 1
fi
sleep 5 # Give etcd a moment to initialize

# 2. Start Placement Driver Cluster
echo "▶️  Starting 3-node Placement Driver cluster..."
PD_INITIAL_MEMBERS="1:127.0.0.1:${PD_RAFT_1},2:127.0.0.1:${PD_RAFT_2},3:127.0.0.1:${PD_RAFT_3}"

# Node 1
PD_DATA_DIR_1=$(mktemp -d /tmp/pd_dev_1.XXXXXX)
./bin/placementdriver \
    --node-id=1 \
    --cluster-id=1 \
    --raft-addr="127.0.0.1:${PD_RAFT_1}" \
    --grpc-addr="127.0.0.1:${PD_GRPC_1}" \
    --data-dir="${PD_DATA_DIR_1}" \
    --initial-members="${PD_INITIAL_MEMBERS}" > /tmp/vectron-pd1.log 2>&1 &

# Node 2
PD_DATA_DIR_2=$(mktemp -d /tmp/pd_dev_2.XXXXXX)
./bin/placementdriver \
    --node-id=2 \
    --cluster-id=1 \
    --raft-addr="127.0.0.1:${PD_RAFT_2}" \
    --grpc-addr="127.0.0.1:${PD_GRPC_2}" \
    --data-dir="${PD_DATA_DIR_2}" \
    --initial-members="${PD_INITIAL_MEMBERS}" > /tmp/vectron-pd2.log 2>&1 &

# Node 3
PD_DATA_DIR_3=$(mktemp -d /tmp/pd_dev_3.XXXXXX)
./bin/placementdriver \
    --node-id=3 \
    --cluster-id=1 \
    --raft-addr="127.0.0.1:${PD_RAFT_3}" \
    --grpc-addr="127.0.0.1:${PD_GRPC_3}" \
    --data-dir="${PD_DATA_DIR_3}" \
    --initial-members="${PD_INITIAL_MEMBERS}" > /tmp/vectron-pd3.log 2>&1 &
sleep 5 # Give PD cluster time to elect a leader

# 3. Start Worker
echo "▶️  Starting Worker service..."
WORKER_DATA_DIR=$(mktemp -d /tmp/worker_dev.XXXXXX)
./bin/worker \
    --node-id=1 \
    --grpc-addr="127.0.0.1:${WORKER_GRPC}" \
    --pd-addrs="${PD_ADDRS}" \
    --data-dir="${WORKER_DATA_DIR}" > /tmp/vectron-worker.log 2>&1 &
sleep 2

# 4. Start Auth Service
echo "▶️  Starting Auth service..."
GRPC_PORT=":${AUTH_GRPC}" HTTP_PORT=":${AUTH_HTTP}" ./bin/authsvc > /tmp/vectron-auth.log 2>&1 &
sleep 2

# 5. Start API Gateway
echo "▶️  Starting API Gateway service..."
GRPC_ADDR="127.0.0.1:${APIGATEWAY_GRPC}" HTTP_ADDR="127.0.0.1:10012" PLACEMENT_DRIVER="${PD_ADDRS}" ./bin/apigateway > /tmp/vectron-apigw.log 2>&1 &
sleep 2

# 6. Start Frontend
echo "▶️  Starting Frontend development server..."
(
    cd auth/frontend
    echo "    (Running npm install in auth/frontend...)"
    npm install > /tmp/vectron-frontend-install.log 2>&1
    VITE_API_BASE_URL="http://localhost:${AUTH_HTTP}" npm run dev -- --port ${FRONTEND_PORT}
)

echo " "
echo "🎉 All services are running!"
echo "-----------------------------------"
echo "Vectron Frontend      > http://localhost:${FRONTEND_PORT}"
echo "Auth Service (HTTP)   > http://localhost:${AUTH_HTTP}"
echo "API Gateway (gRPC)    > 127.0.0.1:${APIGATEWAY_GRPC}"
echo "-----------------------------------"
echo "Logs are being written to /tmp/vectron-*.log"
echo "Press Ctrl+C to stop all services."

# Wait indefinitely until the script is interrupted
wait
