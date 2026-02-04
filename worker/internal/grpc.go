// This file implements the gRPC server for the Worker service.
// It handles incoming RPCs from the API Gateway, and for each request,
// it directs the operation to the correct shard (Raft cluster).
// Write operations are proposed to the shard's Raft log, and read
// operations are performed via linearizable reads on the shard's FSM.

package internal

import (
	"container/heap"
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/lni/dragonboat/v3"
	"github.com/pavandhadge/vectron/shared/proto/worker"
	"github.com/pavandhadge/vectron/worker/internal/shard"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// raftTimeout is the default timeout for Raft proposals.
	raftTimeout = 5 * time.Second
)

var debugLogs = os.Getenv("VECTRON_DEBUG_LOGS") == "1"

type searchHeapItem struct {
	id    string
	score float32
}

type searchMinHeap []searchHeapItem

func (h searchMinHeap) Len() int           { return len(h) }
func (h searchMinHeap) Less(i, j int) bool { return h[i].score < h[j].score }
func (h searchMinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *searchMinHeap) Push(x interface{}) {
	*h = append(*h, x.(searchHeapItem))
}
func (h *searchMinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// GrpcServer implements the gRPC worker.WorkerServiceServer interface.
type GrpcServer struct {
	worker.UnimplementedWorkerServiceServer
	nodeHost     *dragonboat.NodeHost // The Dragonboat node host that manages all shards on this worker.
	shardManager *shard.Manager       // The manager for all shards hosted on this worker.
}

// NewGrpcServer creates a new instance of the gRPC server.
func NewGrpcServer(nh *dragonboat.NodeHost, sm *shard.Manager) *GrpcServer {
	return &GrpcServer{
		nodeHost:     nh,
		shardManager: sm,
	}
}

// StoreVector handles the request to store a vector.
// It marshals the request into a command and proposes it to the target shard's Raft group.
func (s *GrpcServer) StoreVector(ctx context.Context, req *worker.StoreVectorRequest) (*worker.StoreVectorResponse, error) {
	if debugLogs {
		log.Printf("Received StoreVector request for ID: %s on shard %d", req.GetVector().GetId(), req.GetShardId())
	}
	if req.GetVector() == nil {
		return nil, status.Error(codes.InvalidArgument, "vector is nil")
	}

	// Before proposing, check if the shard is ready on this node.
	if !s.shardManager.IsShardReady(req.GetShardId()) {
		return nil, status.Errorf(codes.Unavailable, "shard %d not ready", req.GetShardId())
	}

	// Create the command for the shard's FSM.
	cmd := shard.Command{
		Type:     shard.StoreVector,
		ID:       req.GetVector().GetId(),
		Vector:   req.GetVector().GetVector(),
		Metadata: req.GetVector().GetMetadata(),
	}
	cmdBytes, err := shard.EncodeCommand(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal command: %v", err)
	}

	// Propose the command to the shard's Raft group. This is a blocking call.
	cs := s.nodeHost.GetNoOPSession(req.GetShardId())
	_, err = s.nodeHost.SyncPropose(ctx, cs, cmdBytes)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose StoreVector command: %v", err)
	}

	return &worker.StoreVectorResponse{}, nil
}

// BatchStoreVector handles storing multiple vectors in a single Raft proposal.
func (s *GrpcServer) BatchStoreVector(ctx context.Context, req *worker.BatchStoreVectorRequest) (*worker.BatchStoreVectorResponse, error) {
	if req == nil || len(req.GetVectors()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "vectors are empty")
	}

	if !s.shardManager.IsShardReady(req.GetShardId()) {
		return nil, status.Errorf(codes.Unavailable, "shard %d not ready", req.GetShardId())
	}

	cmdVectors := make([]shard.VectorEntry, 0, len(req.GetVectors()))
	for _, v := range req.GetVectors() {
		if v == nil || v.GetId() == "" || len(v.GetVector()) == 0 {
			return nil, status.Error(codes.InvalidArgument, "vector id or vector data missing")
		}
		cmdVectors = append(cmdVectors, shard.VectorEntry{
			ID:       v.GetId(),
			Vector:   v.GetVector(),
			Metadata: v.GetMetadata(),
		})
	}

	cmd := shard.Command{
		Type:    shard.StoreVectorBatch,
		Vectors: cmdVectors,
	}
	cmdBytes, err := shard.EncodeCommand(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal batch command: %v", err)
	}

	cs := s.nodeHost.GetNoOPSession(req.GetShardId())
	if _, err := s.nodeHost.SyncPropose(ctx, cs, cmdBytes); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose batch StoreVector command: %v", err)
	}

	return &worker.BatchStoreVectorResponse{Stored: int32(len(cmdVectors))}, nil
}

// Search performs a similarity search for a given vector.
// It performs a linearizable read on the target shard's FSM to ensure up-to-date results.
func (s *GrpcServer) Search(ctx context.Context, req *worker.SearchRequest) (*worker.SearchResponse, error) {
	if debugLogs {
		log.Printf("Received Search request on shard %d", req.GetShardId())
	}
	if len(req.GetVector()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "search vector is empty")
	}

	query := shard.SearchQuery{
		Vector: req.GetVector(),
		K:      int(req.GetK()),
	}
	useLinearizable := req.GetLinearizable()

	// If ShardId is 0, it's a broadcast search. Search all shards on this worker.
	if req.GetShardId() == 0 {
		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			topKHeap = &searchMinHeap{}
		)
		heap.Init(topKHeap)

		shardIDs := s.shardManager.GetShards()
		for _, shardID := range shardIDs {
			wg.Add(1)
			go func(id uint64) {
				defer wg.Done()
				var res interface{}
				var err error
				if useLinearizable {
					res, err = s.nodeHost.SyncRead(ctx, id, query)
				} else {
					res, err = s.nodeHost.StaleRead(id, query)
				}
				if err != nil {
					// Log error but don't fail the whole search for one failed shard.
					log.Printf("Failed to search shard %d: %v", id, err)
					return
				}
				searchResult, ok := res.(*shard.SearchResult)
				if !ok {
					log.Printf("Unexpected search result type from shard %d: %T", id, res)
					return
				}

				mu.Lock()
				limit := int(req.GetK())
				if limit <= 0 {
					limit = 10
				}
				for i, resultID := range searchResult.IDs {
					if i >= len(searchResult.Scores) {
						break
					}
					score := searchResult.Scores[i]
					if topKHeap.Len() < limit {
						heap.Push(topKHeap, searchHeapItem{id: resultID, score: score})
						continue
					}
					if topKHeap.Len() > 0 && score > (*topKHeap)[0].score {
						(*topKHeap)[0] = searchHeapItem{id: resultID, score: score}
						heap.Fix(topKHeap, 0)
					}
				}
				mu.Unlock()
			}(shardID)
		}
		wg.Wait()

		resultCount := topKHeap.Len()
		ids := make([]string, resultCount)
		scores := make([]float32, resultCount)
		for i := resultCount - 1; i >= 0; i-- {
			item := heap.Pop(topKHeap).(searchHeapItem)
			ids[i] = item.id
			scores[i] = item.score
		}
		return &worker.SearchResponse{Ids: ids, Scores: scores}, nil

	} else {
		// Original logic for single-shard search
		var res interface{}
		var err error
		if useLinearizable {
			res, err = s.nodeHost.SyncRead(ctx, req.GetShardId(), query)
		} else {
			res, err = s.nodeHost.StaleRead(req.GetShardId(), query)
		}
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to perform search: %v", err)
		}

		searchResult, ok := res.(*shard.SearchResult)
		if !ok {
			return nil, status.Errorf(codes.Internal, "unexpected search result type: %T", res)
		}

		return &worker.SearchResponse{Ids: searchResult.IDs, Scores: searchResult.Scores}, nil
	}
}

// GetVector retrieves a vector by its ID.
// This is a read operation and uses a linearizable read from the FSM.
func (s *GrpcServer) GetVector(ctx context.Context, req *worker.GetVectorRequest) (*worker.GetVectorResponse, error) {
	if debugLogs {
		log.Printf("Received GetVector request for ID: %s on shard %d", req.GetId(), req.GetShardId())
	}

	if !s.shardManager.IsShardReady(req.GetShardId()) {
		return nil, status.Errorf(codes.Unavailable, "shard %d not ready", req.GetShardId())
	}

	// Perform a linearizable read on the shard's state machine.
	res, err := s.nodeHost.SyncRead(ctx, req.GetShardId(), shard.GetVectorQuery{ID: req.GetId()})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to perform GetVector: %v", err)
	}
	if res == nil {
		return nil, status.Errorf(codes.NotFound, "vector with id %s not found", req.GetId())
	}

	getResult, ok := res.(*shard.GetVectorQueryResult)
	if !ok || getResult == nil {
		return nil, status.Errorf(codes.Internal, "unexpected GetVector result type or nil result")
	}

	return &worker.GetVectorResponse{
		Vector: &worker.Vector{
			Id:       req.GetId(),
			Vector:   getResult.Vector,
			Metadata: getResult.Metadata,
		},
	}, nil
}

// DeleteVector deletes a vector by its ID.
// This is a write operation and is proposed to the Raft log.
func (s *GrpcServer) DeleteVector(ctx context.Context, req *worker.DeleteVectorRequest) (*worker.DeleteVectorResponse, error) {
	if debugLogs {
		log.Printf("Received DeleteVector request for ID: %s on shard %d", req.GetId(), req.GetShardId())
	}

	cmd := shard.Command{
		Type: shard.DeleteVector,
		ID:   req.GetId(),
	}
	cmdBytes, err := shard.EncodeCommand(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal command: %v", err)
	}

	cs := s.nodeHost.GetNoOPSession(req.GetShardId())
	// Propose the delete command to the shard's Raft group.
	_, err = s.nodeHost.SyncPropose(ctx, cs, cmdBytes)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose DeleteVector command: %v", err)
	}

	return &worker.DeleteVectorResponse{}, nil
}

// --- Unimplemented Methods ---
// The following methods are part of the WorkerService but are not yet implemented.

func (s *GrpcServer) Put(ctx context.Context, req *worker.PutRequest) (*worker.PutResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Put not implemented")
}

func (s *GrpcServer) Get(ctx context.Context, req *worker.GetRequest) (*worker.GetResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Get not implemented")
}

func (s *GrpcServer) Delete(ctx context.Context, req *worker.DeleteRequest) (*worker.DeleteResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Delete not implemented")
}

func (s *GrpcServer) Status(ctx context.Context, req *worker.StatusRequest) (*worker.StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Status not implemented")
}

func (s *GrpcServer) Flush(ctx context.Context, req *worker.FlushRequest) (*worker.FlushResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Flush not implemented")
}
