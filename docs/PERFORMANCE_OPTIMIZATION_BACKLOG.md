# Performance Optimization Backlog (Current)

Last updated: 2026-02-08

This backlog reflects the current codebase and active profiling documents:

- `PERF_PROFILE_FINDINGS.md`
- `PERF_OPTIMIZATION_TRACKER.md`

## 1. Completed / Landed

- Added broad env-driven tuning surface across gateway/worker/PD (`ENV_SAMPLE.env`).
- Added search and routing cache controls in gateway.
- Added worker and gateway profiling dump hooks (`PPROF_*`).
- Added batching/streaming paths in worker and gateway for heavy data/search operations.

## 2. In Progress

1. Validate production-safe defaults for cache TTL/size per deployment shape.
2. Validate search fanout concurrency under mixed shard distributions.
3. Harden rerank timeout policies to avoid tail-latency amplification.

## 3. Next Priorities

1. Establish reproducible benchmark matrix (dataset size, dims, shard count, concurrency).
2. Wire benchmark outputs to acceptance thresholds per service.
3. Expand contention profiling (mutex/block) in staging-like traffic patterns.

## 4. Decision Rules

- Any optimization claim must be validated with benchmark/profiler evidence.
- Keep unsafe throughput toggles clearly gated by explicit env flags.

## 5. References

- `ENV_SAMPLE.env`
- `profile-benchmark.sh`
- `tests/benchmark/*`
- `PERF_PROFILE_FINDINGS.md`
- `PERF_OPTIMIZATION_TRACKER.md`
