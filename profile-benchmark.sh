#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

timestamp="$(date +%Y%m%d_%H%M%S)"
out_dir="profiles/${timestamp}"
mkdir -p "$out_dir"
export BENCH_PROFILE_DIR="${BENCH_PROFILE_DIR:-${out_dir}}"

ensure_bin() {
  local bin="$1"
  if [ ! -x "$bin" ]; then
    echo "Missing $bin; building with \`make linux\`..."
    make linux
    return
  fi
}

ensure_bin "./bin/apigateway"
ensure_bin "./bin/placementdriver"
ensure_bin "./bin/worker"
ensure_bin "./bin/authsvc"
ensure_bin "./bin/reranker"

export PPROF_MUTEX_FRACTION="${PPROF_MUTEX_FRACTION:-5}"
export PPROF_BLOCK_RATE="${PPROF_BLOCK_RATE:-1}"
export PPROF_CPU_SECONDS="${PPROF_CPU_SECONDS:-15}"
export PPROF_MUTEX_SECONDS="${PPROF_MUTEX_SECONDS:-15}"
export PPROF_BLOCK_SECONDS="${PPROF_BLOCK_SECONDS:-15}"

export MARKET_DIMS="${MARKET_DIMS:-256}"
export MARKET_DATASET="${MARKET_DATASET:-100}"
export MARKET_DURATION_SEC="${MARKET_DURATION_SEC:-60}"
export MARKET_CONCURRENCY="${MARKET_CONCURRENCY:-8}"
export MARKET_READ_RATIO="${MARKET_READ_RATIO:-0.90}"
export MARKET_HOT_RATIO="${MARKET_HOT_RATIO:-0.10}"
export MARKET_HOT_QUERY_RATIO="${MARKET_HOT_QUERY_RATIO:-0.80}"
export MARKET_WRITE_BATCH="${MARKET_WRITE_BATCH:-50}"
export MARKET_TOPK="${MARKET_TOPK:-10}"
export MARKET_CLIENTS="${MARKET_CLIENTS:-8}"

PROFILE_SECONDS="${PROFILE_SECONDS:-15}"

echo "Starting TestMarketTrafficBenchmark (log: ${out_dir}/test.log)..."
go test -v ./tests/benchmark -run TestMarketTrafficBenchmark -timeout 10m | tee "${out_dir}/test.log"

pprof_count=$(find "${out_dir}" -maxdepth 1 -type f -name "*.pprof" | wc -l | tr -d ' ')
if [ "${pprof_count}" -eq 0 ]; then
  echo "ERROR: No .pprof files found in ${out_dir}."
  echo "Ensure services are built with self-dump profiling enabled."
  exit 1
fi

echo "Profiles saved under ${out_dir} (${pprof_count} files)"
