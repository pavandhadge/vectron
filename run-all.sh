#!/bin/bash

# A script to run all Vectron services, including the frontend, for local development.

# --- Cleanup Function ---
# This function will be called when the script exits.
cleanup() {
    echo " "
    echo "🛑 Stopping all services..."

    # Kill background jobs
    kill $(jobs -p) 2>/dev/null

    # Stop etcd container if running
    if podman container exists etcd; then
        podman stop etcd > /dev/null 2>&1
    fi

    # Clean temp dirs
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
export WORKER_GRPC_1=10007
export WORKER_RAFT_1=11007
export WORKER_GRPC_2=10014
export WORKER_RAFT_2=11014
export AUTH_GRPC=10008
export AUTH_HTTP=10009
export APIGATEWAY_GRPC=10010
export FRONTEND_PORT=10011
export RERANKER_PORT=10013
export FEEDBACK_DB_PATH="/tmp/vectron/feedback.db"

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

# Ensure local data directories exist
mkdir -p /tmp/vectron

# --- Logging Helpers ---
# Stream logs to both console and a log file with line buffering.
run_bg() {
    local log_file="$1"
    shift
    stdbuf -oL -eL "$@" 2>&1 | tee -a "$log_file" &
}

# --- Start Services ---

# 1. Start Etcd
# 1. Start Etcd (Podman)
echo "▶️  Starting etcd (Podman)..."

ETCD_DATA_DIR="$HOME/.vectron/etcd"
mkdir -p "$ETCD_DATA_DIR"

if podman container exists etcd; then
    podman start etcd > /dev/null
else
    podman run -d \
        --name etcd \
        -p 2379:2379 \
        -v "$ETCD_DATA_DIR:/etcd-data:Z" \
        quay.io/coreos/etcd:v3.5.0 \
        /usr/local/bin/etcd \
        --data-dir /etcd-data \
        --listen-client-urls http://0.0.0.0:2379 \
        --advertise-client-urls http://0.0.0.0:2379
fi

sleep 5

# 2. Start Placement Driver Cluster
echo "▶️  Starting 3-node Placement Driver cluster..."
PD_INITIAL_MEMBERS="1:127.0.0.1:${PD_RAFT_1},2:127.0.0.1:${PD_RAFT_2},3:127.0.0.1:${PD_RAFT_3}"

# Node 1
PD_DATA_DIR_1=$(mktemp -d /tmp/pd_dev_1.XXXXXX)
run_bg /tmp/vectron-pd1.log ./bin/placementdriver \
    --node-id=1 \
    --cluster-id=1 \
    --raft-addr="127.0.0.1:${PD_RAFT_1}" \
    --grpc-addr="127.0.0.1:${PD_GRPC_1}" \
    --data-dir="${PD_DATA_DIR_1}" \
    --initial-members="${PD_INITIAL_MEMBERS}"

# Node 2
PD_DATA_DIR_2=$(mktemp -d /tmp/pd_dev_2.XXXXXX)
run_bg /tmp/vectron-pd2.log ./bin/placementdriver \
    --node-id=2 \
    --cluster-id=1 \
    --raft-addr="127.0.0.1:${PD_RAFT_2}" \
    --grpc-addr="127.0.0.1:${PD_GRPC_2}" \
    --data-dir="${PD_DATA_DIR_2}" \
    --initial-members="${PD_INITIAL_MEMBERS}"

# Node 3
PD_DATA_DIR_3=$(mktemp -d /tmp/pd_dev_3.XXXXXX)
run_bg /tmp/vectron-pd3.log ./bin/placementdriver \
    --node-id=3 \
    --cluster-id=1 \
    --raft-addr="127.0.0.1:${PD_RAFT_3}" \
    --grpc-addr="127.0.0.1:${PD_GRPC_3}" \
    --data-dir="${PD_DATA_DIR_3}" \
    --initial-members="${PD_INITIAL_MEMBERS}"
sleep 5 # Give PD cluster time to elect a leader

# 3. Start Workers
echo "▶️  Starting 2 Worker nodes..."
WORKER_DATA_DIR_1=$(mktemp -d /tmp/worker_dev_1.XXXXXX)
run_bg /tmp/vectron-worker1.log ./bin/worker \
    --node-id=1 \
    --grpc-addr="127.0.0.1:${WORKER_GRPC_1}" \
    --raft-addr="127.0.0.1:${WORKER_RAFT_1}" \
    --pd-addrs="${PD_ADDRS}" \
    --data-dir="${WORKER_DATA_DIR_1}"

WORKER_DATA_DIR_2=$(mktemp -d /tmp/worker_dev_2.XXXXXX)
run_bg /tmp/vectron-worker2.log ./bin/worker \
    --node-id=2 \
    --grpc-addr="127.0.0.1:${WORKER_GRPC_2}" \
    --raft-addr="127.0.0.1:${WORKER_RAFT_2}" \
    --pd-addrs="${PD_ADDRS}" \
    --data-dir="${WORKER_DATA_DIR_2}"
sleep 2

# 4. Start Auth Service
echo "▶️  Starting Auth service..."
run_bg /tmp/vectron-auth.log env GRPC_PORT=":${AUTH_GRPC}" HTTP_PORT=":${AUTH_HTTP}" ./bin/authsvc
sleep 2

# 5. Start Reranker Service
echo "▶️  Starting Reranker service..."
run_bg /tmp/vectron-reranker.log \
  env \
  RULE_EXACT_MATCH_BOOST=0.3 \
  RULE_TITLE_BOOST=0.2 \
  RULE_METADATA_BOOSTS="verified:0.3,featured:0.2" \
  RULE_METADATA_PENALTIES="deprecated:0.5" \
  ./bin/reranker \
  --port="${RERANKER_PORT}" \
  --strategy="rule" \
  --cache="memory"
sleep 2

# 6. Start API Gateway
echo "▶️  Starting API Gateway service..."
run_bg /tmp/vectron-apigw.log \
  env \
  GRPC_ADDR="127.0.0.1:${APIGATEWAY_GRPC}" \
  HTTP_ADDR="127.0.0.1:10012" \
  PLACEMENT_DRIVER="${PD_ADDRS}" \
  AUTH_SERVICE_ADDR="${AUTH_SERVICE_ADDR}" \
  RERANKER_SERVICE_ADDR="127.0.0.1:${RERANKER_PORT}" \
  ./bin/apigateway
sleep 2

# 7. Start Frontend
echo "▶️  Starting Frontend development server..."
(
    cd auth/frontend
    echo "    (Running npm install in auth/frontend...)"
    npm install | tee -a /tmp/vectron-frontend-install.log

    # Export environment variables for the frontend
    export VITE_AUTH_API_BASE_URL="http://localhost:${AUTH_HTTP}"
    export VITE_APIGATEWAY_API_BASE_URL="http://localhost:10012"
    export VITE_PLACEMENT_DRIVER_API_BASE_URL="http://localhost:${PD_GRPC_1}"

    npm run dev -- --port ${FRONTEND_PORT}
) 2>&1 | tee -a /tmp/vectron-frontend.log &

echo " "
echo "🎉 All services are running!"
echo "-----------------------------------"
echo "Vectron Frontend      > http://localhost:${FRONTEND_PORT}"
echo "Auth Service (HTTP)   > http://localhost:${AUTH_HTTP}"
echo "API Gateway (HTTP)    > http://localhost:10012"
echo "API Gateway (gRPC)    > 127.0.0.1:${APIGATEWAY_GRPC}"
echo "Placement Driver      > 127.0.0.1:${PD_GRPC_1}"
echo "Worker Node 1         > 127.0.0.1:${WORKER_GRPC_1} (raft ${WORKER_RAFT_1})"
echo "Worker Node 2         > 127.0.0.1:${WORKER_GRPC_2} (raft ${WORKER_RAFT_2})"
echo "Reranker Service      > 127.0.0.1:${RERANKER_PORT}"
echo "-----------------------------------"
echo "Frontend Environment Variables:"
echo "VITE_AUTH_API_BASE_URL=http://localhost:${AUTH_HTTP}"
echo "VITE_APIGATEWAY_API_BASE_URL=http://localhost:10012"
echo "VITE_PLACEMENT_DRIVER_API_BASE_URL=http://localhost:${PD_GRPC_1}"
echo "-----------------------------------"
echo "Logs are being written to /tmp/vectron-*.log"
echo "Reranker log: /tmp/vectron-reranker.log"
echo "Press Ctrl+C to stop all services."

# Wait indefinitely until the script is interrupted
wait
