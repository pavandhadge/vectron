# Vectron Worker Memory Analysis & Optimization

## Executive Summary

This document details the investigation into excessive memory usage in the Vectron worker nodes. The worker was consuming 2-3GB+ per node when it should only need ~200-400MB for the actual data being stored.

## The Problem

During benchmark testing with 55,500 vectors (768 dimensions, ~3KB per vector):
- **Expected memory**: ~165 MB (55,500 × 3KB)
- **Actual memory**: 1,500 MB per worker (9x more than expected!)
- **Memory growth**: Exponential growth, ~1GB per minute during inserts

## Investigation Methodology

### 1. Memory Logging
Added detailed memory logging to grpc.go that runs every 10 seconds:
```
MEM: Alloc=1502.3MB Heap=1502.3MB Sys=2719.1MB GC=21
  GC: PauseTotal=66.38ms NextGC=2479.7MB LastGC=12:00:23
  Stack: inuse=4096.0KB
  InFlight: 0
  Shards: 96
  HNSW: nodes=55500 del=0
  Est: vectors=162.6MB overhead=5.3MB total=167.9MB
```

### 2. Disk Usage Analysis
Checked actual on-disk storage to compare with memory:
- Worker data on disk: 237 MB per worker
- But memory showed: 1,500 MB per worker
- Memory was 6x more than disk!

### 3. Configuration Review
Reviewed all env files:
- worker.env - main storage configuration
- placementdriver.env - shard management
- apigateway.env - caching settings

## Root Cause Analysis: The 5x Duplication

Each vector was being stored 5 times across the system:

### 1. HNSW Float32 (Heap - 162 MB)
- Location: `worker/internal/idxhnsw/hnsw.go`
- Storage: `Node.Vec []float32` slice per node
- Size: 55,500 × 768 × 4 bytes = 162 MB

### 2. HNSW Int8 Quantized (Heap - 42 MB)
- Location: `worker/internal/idxhnsw/hnsw.go`
- Storage: `Node.QVec []int8` slice per node
- Only used when: `QuantizeKeepFloatVectors = true` AND `distance = "cosine"`
- Size: 55,500 × 768 × 1 byte = 42 MB

### 3. HNSW Neighbors (Heap - ~20 MB)
- Location: `Node.Neighbors [][]uint32`
- Each node has M connections per layer (M=16, ~6 layers)
- Size: 55,500 × 16 × 6 × 4 bytes = ~20 MB

### 4. ID Maps (Heap - ~6 MB)
- Location: `worker/internal/idxhnsw/hnsw.go`
- Storage: `h.idToUint32` and `h.uint32ToID` maps
- Go maps have high overhead (~50 bytes per entry)

### 5. PebbleDB Vectors (Memory + Disk - ~500 MB)
- Location: `worker/internal/storage/write.go`
- Storage: `batch.Set(vectorKey(id), val, nil)` writes to DB
- Written on EVERY insert via StoreVector()
- Memory: Memtable (~48 MB) + Cache potential (~256 MB)
- Disk: SST files (~165 MB)

### 6. HNSW mmap File (Disk - ~512 MB)
- Location: `worker/internal/idxhnsw/mmap_vectors.go`
- Pre-allocated: 64 MB × 8 active shards = 512 MB
- Should be memory-mapped (not in heap), but wasn't working as expected

## Memory Breakdown Table

| Component | Where | Size | Status |
|-----------|-------|------|--------|
| Float32 vectors | HNSW heap | 162 MB | PROBLEM - should be mmap |
| Int8 quantized | HNSW heap | 42 MB | PROBLEM - could disable |
| Neighbors | HNSW heap | 20 MB | Necessary |
| ID maps | HNSW heap | 6 MB | Necessary |
| Encoded vectors | PebbleDB memtable | 48 MB | Expected |
| PebbleDB cache | PebbleDB cache | 256 MB | CONFIGURABLE |
| SST files | PebbleDB disk | 165 MB | Expected |
| mmap file | Disk | 512 MB | NOT HELPING |

**Total**: ~1,500 MB actual vs ~400 MB expected = **3.75x overhead**

## Why This Happened

### Code Issue 1: Double Vector Storage in HNSW
In `worker/internal/shard/hnsw_defaults.go`:
```go
QuantizeKeepFloatVectors: keepFloatVectors, // was true for cosine
```
This stored BOTH float32 and int8 vectors in memory, doubling the vector storage.

### Code Issue 2: Vectors in Heap Instead of mmap
In `worker/internal/idxhnsw/insert.go`:
```go
func makeVectorCopy(src []float32, h *HNSW) []float32 {
    if h != nil && h.mmapStore != nil {
        // Try mmap first
        if dst, err := h.mmapStore.allocFloat32(len(src)); err == nil && dst != nil {
            copy(dst, src)
            return dst
        }
    }
    // Fallback to heap if mmap fails - vectors end up in heap!
    pooled := getVectorFromPool()
    // ...
}
```
The fallback to heap meant vectors could end up in heap instead of mmap.

### Code Issue 3: Writing Vectors to PebbleDB on Every Insert
In `worker/internal/storage/write.go`:
```go
func (r *PebbleDB) StoreVector(id string, vector []float32, metadata []byte) error {
    // 1. Add to HNSW
    if err := r.hnsw.Add(id, vector); err != nil { ... }
    
    // 2. ALSO write to PebbleDB (DUPLICATE!)
    val, err := encodeVectorWithMeta(vector, metadata, ...)
    batch.Set(vectorKey(id), val, nil)
    batch.Commit(r.writeOpts)
}
```
This created duplicate storage: vectors in HNSW AND in PebbleDB.

### Code Issue 4: High Memory Defaults
In various configurations:
- Pebble cache: 1024 MB (reduced to 256 MB)
- Hot index: 100,000 vectors (disabled)
- Search cache: 50,000 entries (disabled)
- HNSW M parameter: 32 (reduced to 16)
- Cleanup interval: 2 hours (reduced to 30 min)

## Fixes Applied

### Fix 1: Disable QuantizeKeepFloatVectors
File: `worker/internal/shard/hnsw_defaults.go`
```go
keepFloatVectors := false // Changed from quantizeEnabled
```
Saves: ~42 MB (int8 vectors only, no float32)

### Fix 2: Add Memory Logging
File: `worker/internal/grpc.go`
- Added `DebugMemoryStats()` function
- Logs every 10 seconds with detailed breakdown
- Shows: Alloc, Heap, Sys, GC, HNSW nodes, Est memory

### Fix 3: Reduce Memory Configuration
File: `env/worker.env`:
- PEBBLE_CACHE_MB: 1024 → 256
- VECTRON_HOT_INDEX_ENABLED: commented out (disabled)
- VECTRON_WORKER_SEARCH_CACHE_MAX: commented out (disabled)

### Fix 4: HNSW Defaults
File: `worker/internal/shard/hnsw_defaults.go`:
- M: 32 → 16 (halves neighbor memory)
- efConstruction: 200 → 128
- hotMaxSize: default 50,000 → 30,000
- autoTune queue cap: 400,000 → 200,000

### Fix 5: HNSW Cleanup Interval
File: `worker/internal/idxhnsw/internal.go`:
```go
var DefaultCleanupConfig = CleanupConfig{
    Interval:   30 * time.Minute, // was 2 hours
    MaxDeleted: 10_000,          // was 50,000
    BatchSize:  5_000,          // was 10,000
}
```

## Expected Results After Fixes

### Before Fixes (observed):
- Peak memory: 1,500 MB per worker
- For 2 workers: 3,000 MB (~37.5% of 8GB)

### After Fixes (expected):
- HNSW (int8 only): ~42 MB
- Neighbors: ~20 MB
- ID maps: ~6 MB
- PebbleDB memtable: ~48 MB
- Go runtime overhead: ~100-200 MB
- **Total**: ~200-300 MB per worker

For 2 workers: ~500 MB (~6% of 8GB)

## Remaining Issues / Future Work

1. **mmap not working as expected**: Vectors still end up in heap instead of mmap. The fallback code in `makeVectorCopy()` needs investigation.

2. **Go map overhead**: `idToUint32` and `uint32ToID` maps have significant overhead. Consider using a more memory-efficient structure.

3. **PebbleDB cache still reserved**: 256 MB cache is reserved even if not fully used. Consider further reducing.

4. **Raft/Dragonboat memory**: Not measured but likely significant with 96 shards.

5. **No compression at rest**: Vectors are stored as-is. Could compress on-disk storage.

6. **Each shard has own mmap**: 64MB pre-allocated × active shards = significant disk usage that may not be needed.

## Files Modified

1. `worker/internal/grpc.go` - Added memory logging
2. `worker/internal/storage/storage.go` - Added HNSWStats()
3. `worker/internal/idxhnsw/internal.go` - Reduced cleanup interval
4. `worker/internal/shard/hnsw_defaults.go` - Disabled QuantizeKeepFloatVectors, reduced M, efConstruction
5. `worker/internal/shard/hnsw_defaults.go` - Changed hot index default to false
6. `env/worker.env` - Reduced cache, disabled hot index, disabled search cache

## How to Verify

1. Build worker: `cd worker && CGO_ENABLED=0 go build ./...`
2. Run benchmark test
3. Watch memory logs: `grep "MEM:" logs/vectron-worker1-benchmark.log`
4. Check memory stabilizes at ~200-300 MB instead of 1,500 MB

## Key Learnings

1. **Never store duplicates**: Each piece of data should exist in exactly one place in memory
2. **Disable features that double memory**: QuantizeKeepFloatVectors, hot index, search cache
3. **Use mmap properly**: Ensure vectors actually go to mmap, not heap fallback
4. **Configure caches conservatively**: Start small, increase if needed
5. **Monitor memory during development**: Add logging early to catch issues

---

*Document created: 2026-04-10*
*Context: Vectron vector database memory optimization*
*For questions: Check logs in temp_vectron_benchmark/logs/*