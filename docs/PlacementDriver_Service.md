# Placement Driver Service (Current)

Last updated: 2026-02-08

## 1. What It Does

Placement Driver (PD) is the control plane for Vectron.

- stores cluster metadata via Raft
- tracks workers and heartbeats
- assigns shards and replicas
- routes collection/vector ownership queries
- supports worker drain/remove and rebalance operations

## 2. Runtime Defaults

From `placementdriver/cmd/placementdriver/main.go`:

- gRPC: `-grpc-addr` (default `localhost:6001`)
- Raft address: `-raft-addr` (default `localhost:7001`)
- node id: `-node-id`
- cluster id: `-cluster-id`
- initial members: `-initial-members`
- data dir: `-data-dir`

Service starts:

- gRPC server
- shard reconciler
- rebalance manager
- graceful shutdown handlers

## 3. API Surface

From `shared/proto/placementdriver/placementdriver.proto`:

- `GetWorker`
- `RegisterWorker`
- `Heartbeat`
- `ListWorkers`
- `ListWorkersForCollection`
- `DrainWorker`
- `RemoveWorker`
- `Rebalance`
- `CreateCollection`
- `ListCollections`
- `DeleteCollection`
- `GetCollectionStatus`
- `GetLeader`

## 4. Operational Notes

- PD is Raft-backed; metadata changes are proposal-based.
- Workers continuously heartbeat and receive assignment updates.
- API Gateway relies on PD for routing and collection metadata.

## 5. Source of Truth

- `placementdriver/cmd/placementdriver/main.go`
- `placementdriver/internal/server/server.go`
- `placementdriver/internal/server/reconciliation.go`
- `shared/proto/placementdriver/placementdriver.proto`
