# Distributed System Lifecycle Plan

This plan covers practical behaviors for dynamic worker membership (join/leave/failure), shard movement, and safe routing. It is written to be actionable and aligned with industry standards for Raft-based distributed systems.

## Goals
- Safe online scaling (add/remove workers without downtime).
- Predictable behavior during failures.
- Controlled rebalancing with backpressure.
- Clear operational visibility and guardrails.

---

## 1) Worker Lifecycle States
**States:** `joining → ready → draining → removed`

**Rules:**
- `joining`: worker exists in PD but receives no new shard assignments.
- `ready`: eligible for placement and rebalancing targets.
- `draining`: no new assignments; shards are migrated away.
- `removed`: only after all replicas are gone.

**State transitions:**
- `RegisterWorker` → `joining`
- First healthy heartbeat → `ready`
- `DrainWorker` → `draining`
- `RemoveWorker` → `removed` (only when no shards remain)

---

## 2) Add Worker (Online Join)
1. Worker registers → PD records it as `joining`.
2. PD waits for heartbeat → marks `ready`.
3. Rebalancer gradually assigns shards using rate limits.
4. Only after shards are ready does the worker carry normal load.

---

## 3) Remove Worker (Graceful Drain)
1. Admin calls `DrainWorker`.
2. PD marks worker `draining` and rebalancer moves shards off.
3. Once replicas are migrated, admin calls `RemoveWorker`.
4. PD removes worker only if no shards remain (guardrail).

---

## 4) Failure Handling
**Unhealthy:** missed heartbeats.
**Dead:** exceeded cleanup timeout.

Actions:
- Under-replicated shards repaired automatically.
- If dead worker has no shards: remove from PD state.
- If dead worker has shards: do not delete until replicas are restored.

---

## 5) Rebalancing & Placement
**Placement targets only `ready` + healthy workers.**

Rebalancing triggers:
- Worker joins/leaves.
- Hot shards.
- Sustained load imbalance.

Rebalancing rules:
- Rate-limited moves (max concurrent, max per interval).
- Avoid moves during compaction (backoff).
- Move leaders first (future enhancement), then followers.

---

## 6) Routing & Consistency
- API Gateway retries placement lookups on leader changes.
- Linearizable reads can re-resolve after leader change.
- Draining workers can still serve reads until shards migrate.

---

## 7) Operational Safety
- Circuit breakers in worker connection pools.
- Max concurrent shard moves to avoid storms.
- Cooldown between rebalance cycles.

---

## 8) Observability
Expose in management UI:
- Worker state
- Shard distribution
- Under-replicated shards
- Ongoing moves

Alerts:
- Under-replication
- Hot shards
- Leaderless shards

---

## Implementation Phases
**Phase 1 (core safety):**
- Worker state transitions.
- Drain + remove APIs.
- Placement filters (ready only).
- Cleanup dead workers (actual removal).

**Phase 2 (reliability):**
- Rebalancer support for draining workers.
- Health-aware movement.
- Progress tracking for shard migrations.

**Phase 3 (resilience):**
- Leader transfer orchestration.
- Smarter shard movement (hot shard splits).
- Chaos testing suite.

---

## Remaining Items (Advanced / Optional)
- Snapshot progress or match-index barrier before removing a source replica (stronger safety than "running + membership").
- Explicit fencing token per shard/worker beyond the assignments epoch (lease-style or epoch per shard).
- Direct leader-transfer completion acknowledgment (currently inferred via leader ID).
- Stronger partition hardening (e.g., lease-based read/write fencing).
- Shard split/merge for hotspot scalability (non-essential for correctness).
