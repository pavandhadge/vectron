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
  - `go tool pprof -top -nodecount=8 profiles/20260206_234749/worker1_mutex.pprof`
  - `go tool pprof -top -nodecount=8 profiles/20260206_234749/worker1_block.pprof`
  - `go tool pprof -top -nodecount=8 profiles/20260206_234749/apigateway_mutex.pprof`

## Suggested Next Experiments

1. Run a higher load test (e.g., concurrency 32/64) and compare profiles.
2. Disable `LoggingInterceptor` to see lock contention drop in gateway.
3. Raise write batch size and reduce Pebble flush frequency.
4. Isolate HNSW search CPU with a search-only run (no writes) to see pure hot path.
