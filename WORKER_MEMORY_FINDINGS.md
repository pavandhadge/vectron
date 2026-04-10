# Worker Memory And Disk Findings

Date: 2026-04-10

## Summary

The worker was using much more memory and disk than raw vector payload would suggest because the current architecture multiplies fixed overhead by shard count.

Each worker hosts many shards. Each shard currently has:

- its own Pebble DB
- its own HNSW index
- its own Raft/Dragonboat replica state
- its own background loops and snapshot/WAL machinery

So the worker cost is not just `vector_bytes`; it is:

`sum(shard_fixed_overhead) + sum(shard_vector_payload) + raft_overhead`

## What Was Actually Happening

### Disk

The benchmark artifact showed that disk was dominated by per-shard mmap preallocation, not actual vector data.

Observed in `temp_vectron_benchmark`:

- one worker had about `2,147,483,648` bytes of `hnsw_vectors.mmap`
- the same worker had only about `34,018,884` bytes of other shard files

Root cause:

- every shard was creating a fixed `64 MB` mmap file
- the current mmap path only backs float32 HNSW vectors
- the default cosine path keeps quantized int8 vectors only
- so mmap was often allocated but not actually helping the active HNSW representation

### Memory

The earlier memory estimate in logs was undercounting badly because it only estimated HNSW vector bytes and tiny graph overhead.

Large real contributors were:

- per-shard Pebble memtables
- per-shard Pebble table cache / runtime overhead
- Pebble block cache configuration
- per-shard Raft / LogDB overhead
- per-shard HNSW fixed structures

The strongest code-level issue found on the RAM side:

- Pebble block cache was being allocated per shard instead of shared worker-wide

With many shards, that can inflate memory sharply even when vector payload is small.

## Changes Implemented

### 1. Shared Pebble block cache across shards

Implemented in:

- [worker/internal/storage/db.go](/home/pavan/Programming/vectron/worker/internal/storage/db.go)

What changed:

- Pebble cache allocation is now shared at worker scope instead of creating a new cache for every shard DB

Impact:

- bounds cache growth to one worker-wide budget instead of `cache_per_shard * shard_count`

### 2. Disable mmap for int8-only cosine HNSW by default

Implemented in:

- [worker/internal/shard/hnsw_defaults.go](/home/pavan/Programming/vectron/worker/internal/shard/hnsw_defaults.go)

What changed:

- worker now auto-disables mmap when HNSW stores quantized int8 vectors without keeping float vectors
- added `VECTRON_MMAP_INITIAL_MB` support
- reduced default mmap initial size to `4 MB` when mmap is enabled

Impact:

- stops wasting disk on mmap files that are not used by the active HNSW representation
- keeps mmap opt-in / overrideable for float-vector cases

### 3. Better worker memory logging

Implemented in:

- [worker/internal/storage/storage.go](/home/pavan/Programming/vectron/worker/internal/storage/storage.go)
- [worker/internal/grpc.go](/home/pavan/Programming/vectron/worker/internal/grpc.go)

What changed:

- memory log now includes shard-aggregated Pebble stats
- distinguishes quantized shards, float shards, mmap-enabled shards
- shows HNSW estimate plus Pebble memtable / WAL / cache pressure

Impact:

- next benchmark run should show where memory is actually going instead of blaming only HNSW vectors

### 4. Env documentation updated

Updated in:

- [env/worker.env](/home/pavan/Programming/vectron/env/worker.env)

## Not Implemented Yet

These are good ideas but are larger refactors, so they were not done in this pass:

### 1. Worker-level shared Pebble DB instead of one Pebble DB per shard

This would remove a large amount of fixed per-shard overhead.

Status:

- not implemented
- high impact
- medium/high refactor cost

### 2. mmap for quantized int8 vector pages

Right now mmap only helps float32 vector storage. If int8 is the real primary in-memory representation, mmap should support int8 pages directly.

Status:

- not implemented
- good future optimization

### 3. Full segment/index redesign

Examples:

- worker-level shared vector segments
- shard-prefix logical partitioning over shared storage
- lower per-shard runtime duplication

Status:

- not implemented
- architectural redesign

### 4. Streaming HNSW snapshot save path

Current snapshot save still serializes full HNSW into a `bytes.Buffer` before writing to Pebble.

Status:

- not implemented
- could reduce memory spikes during save/rebuild

## Recommended Next Steps

### Immediate

- rebuild and rerun the benchmark
- compare worker memory before and after
- verify disk no longer gets dominated by per-shard `hnsw_vectors.mmap`

### Near term

- add a benchmark that varies shard count while keeping total vector count fixed
- add an assertion for worker-wide cache budget
- tune shard count so each shard carries enough payload to justify its fixed cost

### Bigger architecture work

- move toward worker-level shared Pebble storage with shard prefixes
- support mmap only for representations actually used by search
- keep Pebble as durable source of truth and HNSW as bounded search index

## Expected Outcome From This Pass

This pass should improve the picture materially, but it will not eliminate all overhead because the worker is still fundamentally a multi-shard, multi-DB, multi-index process.

What should improve now:

- disk footprint from useless mmap preallocation
- memory blow-up from per-shard Pebble block caches
- visibility into real memory consumers

What remains structural:

- one HNSW per shard
- one Pebble DB per shard
- one Raft replica stack per shard
