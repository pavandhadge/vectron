// This file implements the gRPC server for the Worker service.
// It handles incoming RPCs from the API Gateway, and for each request,
// it directs the operation to the correct shard (Raft cluster).
// Write operations are proposed to the shard's Raft log, and read
// operations are performed via linearizable reads on the shard's FSM.

package internal

import (
	"context"
	"encoding/json"
	"log"
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
	log.Printf("Received StoreVector request for ID: %s on shard %d", req.GetVector().GetId(), req.GetShardId())
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
	cmdBytes, err := json.Marshal(cmd)
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

// Search performs a similarity search for a given vector.
// It performs a linearizable read on the target shard's FSM to ensure up-to-date results.
func (s *GrpcServer) Search(ctx context.Context, req *worker.SearchRequest) (*worker.SearchResponse, error) {
	log.Printf("Received Search request on shard %d", req.GetShardId())
	if len(req.GetVector()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "search vector is empty")
	}

	// The query to be passed to the FSM's Lookup method.
	query := shard.SearchQuery{
		Vector: req.GetVector(),
		K:      int(req.GetK()),
	}

	// Perform a linearizable read on the shard's state machine.
	// This ensures we are reading the latest committed state.
	res, err := s.nodeHost.SyncRead(ctx, req.GetShardId(), query)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to perform search: %v", err)
	}

	searchResult, ok := res.(*shard.SearchResult)
	if !ok {
		return nil, status.Errorf(codes.Internal, "unexpected search result type: %T", res)
	}

	return &worker.SearchResponse{Ids: searchResult.IDs, Scores: searchResult.Scores}, nil
}

// GetVector retrieves a vector by its ID.
// This is a read operation and uses a linearizable read from the FSM.
func (s *GrpcServer) GetVector(ctx context.Context, req *worker.GetVectorRequest) (*worker.GetVectorResponse, error) {
	log.Printf("Received GetVector request for ID: %s on shard %d", req.GetId(), req.GetShardId())

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
	log.Printf("Received DeleteVector request for ID: %s on shard %d", req.GetId(), req.GetShardId())

	cmd := shard.Command{
		Type: shard.DeleteVector,
		ID:   req.GetId(),
	}
	cmdBytes, err := json.Marshal(cmd)
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
