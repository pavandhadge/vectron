# Perf Profile Findings (Self-Dump PPROF)

**Run Timestamp:** 2026-02-06 23:47–23:48 IST  
**Profile Dir:** `profiles/20260206_234749`  
**Workload:** Market benchmark (dims=256, dataset=100, duration=60s, concurrency=8, reads=90%, writes=10%, batch=50, topK=10)

## What Was Collected

Profiles were self-dumped by each service (no HTTP/curl). Files present:

- API Gateway: `apigateway_cpu.pprof`, `apigateway_mutex.pprof`, `apigateway_block.pprof`
- Placement Driver: `pd1_cpu.pprof`, `pd2_cpu.pprof`, `pd3_cpu.pprof` (+ mutex/block)
- Worker 1/2: `worker1_cpu.pprof`, `worker1_mutex.pprof`, `worker1_block.pprof`, `worker2_cpu.pprof`, `worker2_mutex.pprof`, `worker2_block.pprof`

## Summary Of Findings

### 1) Worker CPU is dominated by syscall + cgo + HNSW search

From `worker1_cpu.pprof` and `worker2_cpu.pprof`:

- `internal/runtime/syscall.Syscall6` ~21% flat CPU
- `runtime.cgocall` ~12% flat (likely AVX/CGO quantization or other C-path)
- `idxhnsw.(*HNSW).searchLayer` ~5–7% flat, ~33–39% cumulative
- `container/heap.down` ~3% flat
- `idxhnsw.(*HNSW).getNode` ~2–3% flat

Interpretation:
- Search path is CPU-hot, dominated by syscall/cgo overhead + HNSW search/heap operations.
- Heap ops and node fetch are a significant share; optimizing heap/selection and node storage is still a high-leverage area.

### 2) Worker mutex profile shows heavy lock contention around Pebble and Dragonboat

From `worker1_mutex.pprof`:

- `sync.(*Mutex).Unlock` ~44.5% flat
- `sync.(*RWMutex).Unlock` ~38.8% flat
- `sync.(*RWMutex).RUnlock` ~16.3% flat
- Cumulative:
  - `pebble.(*DB).Flush / AsyncFlush`
  - `dragonboat.(*NodeHost).StaleRead`
  - `dragonboat.(*NodeHost).StartOnDiskCluster`

Interpretation:
- Mutex contention is strong in Pebble flush paths and Dragonboat read path.
- Suggests write path and raft-managed read path are lock-heavy, likely contributing to lower concurrency throughput.

### 3) Worker block profile shows most time waiting on select/cond

From `worker1_block.pprof`:

- `runtime.selectgo` ~78% flat
- `sync.(*Cond).Wait` ~15% flat
- Remainder: channel receive / waitgroup / Pebble commit pipeline

Interpretation:
- Many goroutines are blocked on channels/condvars (coordination/queues).
- Suggests channel fan-in and worker pipeline serialization are gating concurrency.

### 4) API Gateway CPU is comparatively light; overhead mostly syscall/runtime

From `apigateway_cpu.pprof`:

- `internal/runtime/syscall.Syscall6` ~21% flat
- `runtime.memclrNoHeapPointers` ~6% flat
- Smaller amounts in JSON validation, grpc header handling, sha256 block.

Interpretation:
- Gateway is not the CPU bottleneck at this load; majority time is syscall/runtime overhead.
- Optimization impact is likely higher on worker side.

### 5) API Gateway mutex profile points to logging interceptor & redis client

From `apigateway_mutex.pprof`:

- `sync.(*Mutex).Unlock` ~93% flat
- Cumulative:
  - `middleware.LoggingInterceptor`
  - `apigateway._VectronService_Search_Handler`
  - `redis.(*Client).Process`

Interpretation:
- Logging interceptor introduces lock contention on the hot path.
- Redis client path also contributes to mutex delay (lock contention inside client).

### 6) Placement driver is not a CPU hotspot in this run

From `pd1_cpu.pprof`:

- `Syscall6` ~40% flat
- `runtime.futex` ~13% flat
- Minor Dragonboat engine work.

Interpretation:
- PD isn’t the performance bottleneck for this workload.

## Implications (Where The Throughput Ceiling Likely Is)

1. Worker search path (HNSW) and syscalls/cgo dominate CPU:
   - Optimize heap usage or replace with partial selection where possible.
   - Reduce syscall frequency (batching I/O, fewer small writes/reads).
   - Reduce cgocall overhead (vector quantization/hot loop).

2. Worker lock contention in Pebble/Dragonboat:
   - Reduce flush frequency; batch writes aggressively.
   - Defer / coalesce raft writes.
   - Investigate Dragonboat stale read usage to avoid heavy locks.

3. Worker blocked on select/cond:
   - Indicates pipeline contention, channel bottlenecks, or fan-in throttling.
   - Consider widening queues or using sharded workers to reduce coordination.

4. API Gateway lock contention in logging:
   - If `LoggingInterceptor` runs on every request, it can add significant contention.
   - Consider log sampling or disabling in benchmark runs.

## Commands Used (for reproducibility)

- CPU:
  - `go tool pprof -top -nodecount=8 profiles/20260206_234749/worker1_cpu.pprof`
  - `go tool pprof -top -nodecount=8 profiles/20260206_234749/worker2_cpu.pprof`
  - `go tool pprof -top -nodecount=8 profiles/20260206_234749/apigateway_cpu.pprof`
  - `go tool pprof -top -nodecount=8 profiles/20260206_234749/pd1_cpu.pprof`
- Mutex/Block:
- 
  - `go tool pprof -top -nodecount=8 profiles/20260206_234749/worker1_mutex.pprof`
  - `go tool pprof -top -nodecount=8 profiles/20260206_234749/worker1_block.pprof`
  - `go tool pprof -top -nodecount=8 profiles/20260206_234749/apigateway_mutex.pprof`

## Suggested Next Experiments

1. Run a higher load test (e.g., concurrency 32/64) and compare profiles.
2. Disable `LoggingInterceptor` to see lock contention drop in gateway.
3. Raise write batch size and reduce Pebble flush frequency.
4. Isolate HNSW search CPU with a search-only run (no writes) to see pure hot path.

---

## Deep Dive Findings (HNSW + End-to-End Profiling)

This section is intentionally more granular than the summary above and is based on the same profile set in `profiles/20260206_234749`. The goal is to map where time is spent, why it is spent there, and what is likely wasted effort vs. unavoidable waiting in this benchmark.

### A) Worker CPU Hot Path: HNSW Search

The worker CPU profiles show a clear, repeatable pattern:

1. `idxhnsw.(*HNSW).searchLayer` dominates cumulative time, with repeated heap operations (`container/heap.down`, `container/heap.up`) and node access (`idxhnsw.(*HNSW).getNode`).
2. `runtime.mallocgc` and `mallocgcSmallScanNoHeader` are non-trivial, and `runtime.memclrNoHeapPointers` appears in the top list.
3. `runtime.cgocall` and syscalls take a surprisingly large flat share, suggesting either:
   - vector ops that route through CGO/assembly, or
   - heavy IO or syscall-heavy code paths inside the search loop.

Interpretation:

1. The HNSW search is compute-heavy but also allocation-heavy. The heap-based candidate lists and visited sets are likely being allocated per query.
2. The `container/heap` traffic implies that candidate management is done via standard heap operations rather than a more specialized data structure. That is fine but comes with many comparisons and allocations.
3. `cgocall` overhead indicates that the search path likely calls into non-Go code or uses AVX/CGO for vector ops. This overhead is acceptable if it provides strong speedups, but it becomes wasteful if it is called too frequently with small payloads.

Likely wasted effort:

1. Per-query allocation churn in HNSW search structures.
2. Repeated heap operations with small heaps (common in low dataset sizes) that could be optimized by cheaper selection logic.
3. Excessive syscall/CGO transitions when batching could amortize them.

Potential fixes:

1. Reuse search buffers and candidate/visited data structures with `sync.Pool`.
2. Replace heap-based candidate management with a bounded array + partial select for small `ef` sizes.
3. Batch vector distance computations in a single CGO call or pure Go SIMD block to reduce transitions.

### B) Worker Block Profiles: Where Goroutines Wait

Both worker block profiles (`worker1_block.pprof`, `worker2_block.pprof`) are dominated by:

1. `runtime.selectgo` (~77–79% flat)
2. `sync.(*Cond).Wait` (~15% flat)
3. `runtime.chanrecv1`, `runtime.chanrecv2`

Interpretation:

1. Goroutines are frequently parked waiting on channels/condvars.
2. This is not necessarily “wasted” if it reflects healthy backpressure, but it can also indicate pipeline bottlenecks or queues that are draining slower than expected.
3. The presence of Pebble `LogWriter.flushLoop` in the same block profiles suggests the storage layer is part of the stall chain.

Likely wasted effort:

1. Idle goroutines waiting on queue work that is gated by the storage flush path.
2. Over-serialization inside the raft + pebble pipeline which delays availability of completed writes.

Potential fixes:

1. Increase write batching or reduce flush frequency to lower `Cond.Wait` time.
2. Reduce per-request synchronization in worker pipeline by sharding queues or using separate goroutine pools for read vs. write paths.

### C) Worker Mutex Profiles: Lock Contention Hotspots

Mutex profiles show heavy contention in:

1. `sync.(*RWMutex).Unlock` and `sync.(*RWMutex).RUnlock`
2. `pebble.(*DB).Flush` and `pebble.(*DB).AsyncFlush`
3. `dragonboat.(*NodeHost).StaleRead` and `rsm.(*StateMachine).Lookup`

Interpretation:

1. Reads and writes are both touching a shared lock, likely on the state machine or DB.
2. The repeated appearance of `Flush` suggests that write durability or commit path is forcing contention.
3. `StaleRead` shows that even read paths pay a synchronization cost.

Likely wasted effort:

1. Frequent flush operations during a benchmark with small dataset size (100 vectors) is likely overkill.
2. Lock contention suggests under-sharding: multiple goroutines fighting over a shared state machine or shared Pebble instance.

Potential fixes:

1. Increase write batch size and delay flush to coalesce IO.
2. Evaluate whether some read paths can bypass raft or use a read-through cache.
3. Consider sharding state machine/pebble instances by partition or collection to reduce lock contention.

### D) API Gateway: Where It Stalls

The gateway profiles show:

1. Low CPU usage relative to workers.
2. Heavy block time in `runtime.selectgo` and `sync.WaitGroup.Wait`.
3. Mutex contention in `LoggingInterceptor` and Redis client process path.

Interpretation:

1. Gateway is mostly waiting on downstream worker responses.
2. Logging and Redis add lock contention, but they are not the throughput ceiling.

Likely wasted effort:

1. Lock contention in logging on the hot path for every request.
2. Redis connection pool locks during cache checks.

Potential fixes:

1. Log sampling or async logging during high throughput runs.
2. Increase Redis pool size or reduce per-request cache round-trips.

### E) Placement Driver: Mostly Idle

The PD block profiles show `runtime.selectgo` dominating >97% of block time. CPU usage is minimal, and mutex contention is negligible.

Interpretation:

1. PD is not the bottleneck at this workload.
2. Any optimization here would not shift throughput unless workload or cluster size increases.

### F) End-to-End Latency Chain

Putting it together:

1. API Gateway waits on worker responses.
2. Worker search path is CPU hot (HNSW + heap).
3. Worker write path is slow due to flush/raft contention.
4. Mutex contention and blocking in worker pipelines likely inflate P95+ latencies.

### G) What Is “Wasted” vs. “Necessary”

Wasted or reducible:

1. Per-request allocations in search, especially heap/visited sets.
2. Overly frequent flushes in Pebble/raft pipeline.
3. Logging contention on gateway hot path.
4. Excessive synchronization in read path (`StaleRead` lock cost).

Necessary or expected for this workload:

1. `selectgo` and `Cond.Wait` while idle or under small dataset load.
2. Syscall time when operating on real network/IO.
3. CGO overhead if vector ops are optimized and provide net speedups.

### H) Targeted Fixes With the Highest ROI

1. HNSW search allocations:
   - Pool candidate and visited buffers.
   - Avoid heap for small `ef` values by using a preallocated slice and partial selection.
2. Write batching and flush tuning:
   - Increase batch size, reduce flush frequency, or make flush async with fewer forced sync points.
3. Read path lock contention:
   - Investigate `StaleRead` usage and minimize lock acquisition on read hot path.
4. Gateway logging:
   - Sampling or async log writes.

### I) Profiling Extensions To Validate Fixes

1. Capture `heap` profile during the same run to validate allocation hotspots.
2. Capture `trace` to visualize blocking and scheduler behavior across goroutines.
3. Run an isolated search-only benchmark to separate HNSW cost from storage/raft overhead.
4. Run an isolated write-only benchmark to quantify Pebble/raft contention impact.
