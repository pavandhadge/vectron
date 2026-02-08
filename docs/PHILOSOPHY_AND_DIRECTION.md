# Vectron Engineering Philosophy and Direction

Last updated: 2026-02-08

This document captures the engineering philosophy behind Vectron and the direction guiding architectural decisions.

## 1. Philosophy: What We Optimize For

Vectron is built around five priorities.

1. Contract clarity over convenience
2. Failure isolation over tightly coupled speed hacks
3. Explicit tradeoffs over hidden behavior
4. Operability over theoretical elegance
5. Evolvability over one-time optimization

These priorities explain many implementation choices across gateway, PD, and workers.

## 2. Core Principles

### 2.1 Control and Data Planes Must Be Separated

Why:

- placement and metadata consistency has different scaling/failure needs than vector IO/search
- independent scaling and blast-radius reduction

How it manifests:

- PD owns placement metadata and worker topology state
- worker shard groups own replicated vector state

### 2.2 Public Contract Should Be Stable Even If Internals Change

Why:

- SDK users and external clients need predictable APIs
- internal schemas should evolve freely

How it manifests:

- separate `apigateway.proto` and `worker.proto`
- translation layer in gateway

### 2.3 Every Critical Path Must Have Knobs

Why:

- workload shapes differ dramatically
- defaults are never universally optimal

How it manifests:

- large env-driven tuning surface (`ENV_SAMPLE.env`)
- consistency, cache, fanout, rerank, and storage controls

### 2.4 Consistency Is a Policy, Not a Dogma

Why:

- some workloads need strict reads
- some workloads need lowest latency

How it manifests:

- configurable linearizable/non-linearizable search semantics
- Raft-guaranteed write path regardless of read policy

### 2.5 Operations Are a First-Class Product Surface

Why:

- distributed systems fail in production-specific ways
- debuggability determines real reliability

How it manifests:

- management endpoints
- health and alert surfaces
- built-in profiling hooks and detailed runtime flags

## 3. Architectural Tradeoffs (Intentional)

### 3.1 Why API Gateway Is Feature-Rich

Decision:

- keep protocol translation, auth, fanout, cache, and optional rerank orchestration at the edge.

Benefit:

- downstream services stay focused and simpler.

Cost:

- gateway complexity rises and must be disciplined with strong docs/tests.

### 3.2 Why PD Uses Reconciliation and Rebalancing Loops

Decision:

- continuously reconcile desired and actual cluster state.

Benefit:

- less manual intervention for common repair paths.

Cost:

- background automation needs safety rails (backoff, health checks, move limits).

### 3.3 Why Worker Uses Multi-Raft + Local Index

Decision:

- independent shard Raft groups with local HNSW/Pebble state.

Benefit:

- shard-level fault isolation and parallelism.

Cost:

- more complex membership and recovery behavior.

## 4. Product Direction (Current)

### 4.1 Near-Term Direction

1. Close correctness gaps at boundaries
- complete payload/metadata translation parity
- tighten management endpoint security posture by default patterns

2. Improve production confidence
- benchmark-driven default tuning
- stronger SLO-focused telemetry and alert fidelity

3. Harden distributed behavior
- continue rebalancer safety and determinism improvements
- improve cross-shard search observability/debuggability

### 4.2 Mid-Term Direction

1. Relevance pipeline maturity
- extend reranker beyond rule strategy with production-safe implementations
- connect feedback loop to measurable ranking improvements

2. Topology specialization
- mature write/search role separation patterns
- improve routing and placement policy controls for heterogeneous clusters

3. Deployment ergonomics
- stronger operator docs and packaged deployment patterns
- explicit production profiles for common workload types

## 5. What We Avoid (Anti-Philosophy)

- hidden consistency changes at runtime without operator control
- "smart" behavior without observability hooks
- coupling client contracts to worker internals
- conflating roadmap ideas with implemented functionality
- optimistic claims without benchmark or trace evidence

## 6. Decision Quality Bar

A major architecture decision should include:

1. clear problem statement
2. alternatives considered
3. chosen approach and explicit tradeoffs
4. rollout and rollback strategy
5. observability and failure-mode impact
6. documentation updates (this folder)

If a proposal cannot pass this bar, it should not merge as an architecture change.

## 7. Documentation Direction for Website Use

Recommended website hierarchy:

1. `docs/Vectron_Project_Overview.md` (entry)
2. `docs/ARCHITECTURE_DEEP_DIVE.md` (primary deep technical page)
3. `docs/API.md` (contract summary)
4. service pages (`APIGateway_Service.md`, `Worker_Service.md`, etc.)
5. `docs/PHILOSOPHY_AND_DIRECTION.md` (engineering narrative / contributor orientation)

## 8. Contributor Guidance

Before shipping cross-service changes:

- update proto contracts first if needed
- document migration impact (SDK, gateway, worker, PD)
- capture perf and correctness evidence
- update `docs/CURRENT_STATE.md` and relevant deep-dive sections

## 9. Bottom Line

Vectron’s direction is not "add features fastest." It is:

- preserve clear contracts
- keep distributed behavior explainable
- make performance tunable and measurable
- grow capabilities without compromising operator trust

That philosophy is the main architectural constraint and the main long-term advantage.
