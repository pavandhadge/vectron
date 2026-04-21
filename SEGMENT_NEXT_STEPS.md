# Segment Next Steps

## Goal

Current segment path good enough to run:
- live worker uses segment index
- mutable ingest async
- immutable payload files real
- compaction real
- segment memory logging real

Next big gains:
- smarter compaction policy
- segment pruning / search planning
- richer mmap / int8 payload layout

This file says what to build later. No fluff. Only work plan.

## 1. Smarter Compaction Policy

### Problem now

Current compaction policy simple:
- take first `CompactionFanIn` immutable segments
- merge all live docs
- drop tombstoned docs

Weak points:
- no size tiers
- no age tiers
- no write amplification control
- can compact big segments too early
- no budget / throttle
- no overlap awareness

### Target policy

Use tiered compaction first. Not LSM-perfect. Pragmatic.

Per segment track:
- `VectorCount`
- `BytesEstimate`
- `CreatedAt`
- `SealedAt`
- tombstone ratio
- query hit count later if wanted

Choose candidates by rules:
1. only immutable segments
2. skip segment if too new
3. skip segment if too big
4. prefer many small segments in same size tier
5. prefer segments with high tombstone waste

Suggested tiers:
- Tier 0: `< 32 MB`
- Tier 1: `32-128 MB`
- Tier 2: `128-512 MB`
- Tier 3: `> 512 MB`

Suggested first trigger:
- if tier has `>= 4` segments, compact oldest `4`
- or if total tombstone ratio across candidates `>= 0.20`

### Implementation plan

Add to [worker/internal/segment/types.go](/home/pavan/Programming/vectron/worker/internal/segment/types.go):
- optional fields in `SegmentMeta`
  - `ApproxTombstones int64`
  - `CompactedFrom []SegmentID`

Add to [worker/internal/segment/seal.go](/home/pavan/Programming/vectron/worker/internal/segment/seal.go):
- `pickCompactionCandidates(manifest *ShardManifest) []SegmentID`
- `segmentSizeTier(meta SegmentMeta) int`
- `segmentTooLarge(meta SegmentMeta) bool`
- `segmentTooYoung(meta SegmentMeta) bool`

Replace current:
- `segmentsToCompact := manifest.ImmutableSegments[:threshold]`

With:
- load metas for immutable segments
- group by tier
- pick best candidate set

Suggested env knobs:
- `VECTRON_SEGMENT_COMPACT_MIN_AGE_SEC`
- `VECTRON_SEGMENT_COMPACT_MAX_SEG_MB`
- `VECTRON_SEGMENT_COMPACT_TOMBSTONE_RATIO`
- `VECTRON_SEGMENT_COMPACT_FANIN`
- `VECTRON_SEGMENT_COMPACT_MAX_BYTES`

### Safety rules

- compaction writes new segment first
- manifest swap last
- old segment delete only after manifest save
- on failure: keep old segments untouched

### Nice follow-up

Background compaction should skip when:
- ingest queue high
- heap pressure high
- recent compaction already running

Add:
- `VECTRON_SEGMENT_COMPACT_PAUSE_PENDING`
- `VECTRON_SEGMENT_COMPACT_PAUSE_HEAP_MB`

## 2. Segment Pruning / Search Planning

### Problem now

Current search path naive:
- query mutable
- query every immutable
- merge top-k
- rerank

Weak points:
- too much fanout as segment count grows
- no cheap skip
- no candidate budgeting per segment quality
- no query planner

### Target planner

Search should rank segments before querying them.

Per segment track:
- size
- age
- last query hits
- min/max doc ID if useful
- optional centroid later

Planner phases:
1. always query mutable
2. score immutable segments
3. query top N segments first
4. early stop if candidate heap already strong

### First useful planner

No centroid yet. Keep it simple.

Score segment by:
- smaller segments slightly lower priority than medium segments
- very old tiny segments lower priority
- segments with recent hits higher priority
- segments with high tombstone ratio lower priority

Suggested first planner:
- if immutable count `<= 4`, query all
- else query top `min(8, immutableCount)` first
- if merged candidate count `< 2*k`, continue with next tier

### Better planner later

Add centroid file per immutable segment:
- `centroid.bin`
- 1 quantized or float centroid vector

Then planner:
- compute query-to-centroid distance
- rank segments by centroid distance
- query nearest segments first

This gives real segment pruning.

### Implementation plan

Add to [worker/internal/segment/immutable.go](/home/pavan/Programming/vectron/worker/internal/segment/immutable.go):
- centroid field
- optional load from `centroid.bin`

Add to [worker/internal/segment/seal.go](/home/pavan/Programming/vectron/worker/internal/segment/seal.go):
- compute centroid while sealing
- write `centroid.bin`

Add to [worker/internal/segment/search.go](/home/pavan/Programming/vectron/worker/internal/segment/search.go):
- `rankSegments(query []float32, segs []*ImmutableSegment) []*ImmutableSegment`
- `queryBudget(k int, segCount int) int`
- `shouldContinueFanout(...) bool`

Suggested env knobs:
- `VECTRON_SEGMENT_SEARCH_MAX_FANOUT`
- `VECTRON_SEGMENT_SEARCH_MIN_FANOUT`
- `VECTRON_SEGMENT_SEARCH_EARLY_STOP_FACTOR`
- `VECTRON_SEGMENT_CENTROID_ENABLED`

### Metrics to log

Add to memory/debug log later:
- queried segments
- skipped segments
- planner mode
- average fanout per query
- rerank candidates

## 3. Richer mmap / int8 Payload Layout

### Problem now

Current immutable payload format:
- `vectors.bin` float32
- `offsets.bin` uint64 offsets
- `ids.json`

Weak points:
- float32 payload bigger than needed
- JSON IDs waste space
- no direct int8 query path
- rerank always decodes float vectors

### Target layout

Split payload into two modes:

Mode A: search payload
- int8 quantized vectors
- fixed-width rows if dimension fixed
- mmap-friendly

Mode B: exact rerank payload
- optional float32
- only if exact rerank needed

This matches serious ANN systems:
- cheap compact search representation
- optional richer exact payload

### File design

For fixed dimension shards:
- `qvectors.bin`
  - raw int8 rows, size = `count * dim`
- `qmeta.bin`
  - per vector scale / norm if needed
- `fvectors.bin`
  - optional float32 rows
- `ids.bin`
  - length-prefixed or offset table, not JSON
- `id_offsets.bin`
  - offsets into `ids.bin`

If dim fixed, `offsets.bin` for vectors can go away.
Vector location:
- int8 row offset = `idx * dim`
- float row offset = `idx * dim * 4`

Much better than variable-size float payload.

### Query use

For cosine search:
- load `qvectors.bin` with mmap
- distance/rerank stage 1 on int8 directly
- if exact rerank enabled, fetch float row from `fvectors.bin`

For euclidean:
- keep float payload unless quantized path added

### Implementation plan

Add new payload version in meta:
- `PayloadVersion int`
- `PayloadEncoding string`

Suggested encodings:
- `float32_row`
- `int8_row`
- `int8_row_plus_float32_row`

Update [worker/internal/segment/seal.go](/home/pavan/Programming/vectron/worker/internal/segment/seal.go):
- write int8 row file for cosine normalized mode
- optionally write float row file
- stop writing JSON ids

Update [worker/internal/segment/immutable.go](/home/pavan/Programming/vectron/worker/internal/segment/immutable.go):
- mmap int8 row file
- direct row decode by index
- direct cosine distance on int8 row
- optional exact float rerank

### Data structures

Add:
- `payloadEncodingFloat32Row`
- `payloadEncodingInt8Row`
- `payloadEncodingInt8PlusFloat32`

In immutable segment:
- `qvectorsMM []byte`
- `fvectorsMM []byte`
- `idOffsets []uint32` or `[]uint64`
- `idsMM []byte`

### Suggested migration

Do not migrate old segments in place.

Rules:
- old segments still readable
- new sealed segments write new payload version
- compaction rewrites old segments into new payload version

This gives gradual migration for free.

## Recommended Order

If doing later, do in this order:

1. smarter compaction candidate picker
2. centroid-based segment planner
3. int8 row payload format
4. optional float rerank payload
5. metrics/logging polish

## Success Criteria

### Compaction
- fewer tiny immutable segments over time
- tombstone-heavy segments disappear
- no manifest corruption

### Search planning
- average queried segment count drops
- latency stable as segment count grows
- recall not badly damaged

### Payload layout
- immutable payload disk smaller
- RSS lower
- rerank faster
- fewer heap allocations on immutable query path

