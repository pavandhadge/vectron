# Distributed System Plan (Current)

Last updated: 2026-02-08

This file now tracks implemented distributed behavior and near-term work, replacing older speculative planning text.

## 1. Implemented Today

- PD is a Raft-backed control plane with leader-based metadata updates.
- Workers register to PD, heartbeat, and receive shard assignments.
- Workers host replicated shard groups via Dragonboat.
- API Gateway resolves shard routing via PD and forwards to workers.
- Search fanout and streaming response path exist in gateway.
- Worker search-only mode exists (`VECTRON_SEARCH_ONLY=1`).

## 2. Current Operational Patterns

- Control-plane metadata consistency via PD Raft.
- Data-plane write consistency via shard-level Raft in workers.
- Dynamic assignment reconciliation in PD and worker managers.
- Worker role preferences in gateway (`PREFER_SEARCH_ONLY_WORKERS`).

## 3. Near-Term Plan Items

1. Expand automated rebalance policy controls and safety rails.
2. Improve cross-shard search observability and failure diagnostics.
3. Continue performance tuning from profiler-driven findings.
4. Tighten docs/tests around search-only deployment topologies.

## 4. Planning Rules

- Treat `shared/proto/*` and entrypoint code as current truth.
- Mark future ideas explicitly as planned; do not describe as implemented.

## 5. Source of Truth

- `placementdriver/internal/server/*`
- `worker/internal/shard/*`
- `apigateway/cmd/apigateway/main.go`
- `docs/CURRENT_STATE.md`
