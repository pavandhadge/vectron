package internal

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/lni/dragonboat/v3"
	"github.com/pavandhadge/vectron/worker/internal/shard"
	"github.com/pavandhadge/vectron/worker/proto/worker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	raftTimeout = 5 * time.Second
)

// GrpcServer is the gRPC server for the worker.
type GrpcServer struct {
	worker.UnimplementedWorkerServiceServer
	nodeHost     *dragonboat.NodeHost
	shardManager *shard.Manager
}

// NewGrpcServer creates a new instance of the gRPC server.
func NewGrpcServer(nh *dragonboat.NodeHost, sm *shard.Manager) *GrpcServer {
	return &GrpcServer{
		nodeHost:     nh,
		shardManager: sm,
	}
}

// StoreVector stores a vector by proposing a command to the Raft group.
func (s *GrpcServer) StoreVector(ctx context.Context, req *worker.StoreVectorRequest) (*worker.StoreVectorResponse, error) {
	log.Printf("Received StoreVector request for ID: %s on shard %d", req.GetVector().GetId(), req.GetShardId())
	if req.GetVector() == nil {
		return nil, status.Error(codes.InvalidArgument, "vector is nil")
	}

	if !s.shardManager.IsShardReady(req.GetShardId()) {
		return nil, status.Errorf(codes.Unavailable, "shard %d not ready", req.GetShardId())
	}

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

	cs := s.nodeHost.GetNoOPSession(req.GetShardId())
	// Propose the command to the shard's Raft group.
	_, err = s.nodeHost.SyncPropose(ctx, cs, cmdBytes)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose command: %v", err)
	}

	return &worker.StoreVectorResponse{}, nil
}

// Search performs a linearizable search on the correct shard.
func (s *GrpcServer) Search(ctx context.Context, req *worker.SearchRequest) (*worker.SearchResponse, error) {
	log.Printf("Received Search request on shard %d", req.GetShardId())
	if len(req.GetVector()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "search vector is empty")
	}

	query := shard.SearchQuery{
		Vector: req.GetVector(),
		K:      int(req.GetK()),
	}

	// Perform a linearizable read on the shard's state machine.
	res, err := s.nodeHost.SyncRead(ctx, req.GetShardId(), query)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to perform search: %v", err)
	}

	ids, ok := res.([]string)
	if !ok {
		return nil, status.Errorf(codes.Internal, "unexpected search result type: %T", res)
	}

	return &worker.SearchResponse{Ids: ids}, nil
}

// --- Other methods (GetVector, DeleteVector, etc.) would follow a similar pattern ---
// For brevity, they are omitted here but would need to be updated to use
// SyncPropose for writes and SyncRead for reads, targeting the specific shard_id.
// The existing implementations in the file are incorrect for a multi-shard system.

func (s *GrpcServer) GetVector(ctx context.Context, req *worker.GetVectorRequest) (*worker.GetVectorResponse, error) {
	log.Printf("Received GetVector request for ID: %s on shard %d", req.GetId(), req.GetShardId())

	if !s.shardManager.IsShardReady(req.GetShardId()) {
		return nil, status.Errorf(codes.Unavailable, "shard %d not ready", req.GetShardId())
	}

	res, err := s.nodeHost.SyncRead(ctx, req.GetShardId(), shard.GetVectorQuery{ID: req.GetId()})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to perform GetVector: %v", err)
	}

	if res == nil {
		return nil, status.Errorf(codes.NotFound, "vector with id %s not found", req.GetId())
	}

	getResult, ok := res.(*shard.GetVectorQueryResult)
	if !ok {
		return nil, status.Errorf(codes.Internal, "unexpected GetVector result type: %T", res)
	}

	return &worker.GetVectorResponse{
		Vector: &worker.Vector{
			Id:       req.GetId(),
			Vector:   getResult.Vector,
			Metadata: getResult.Metadata,
		},
	}, nil
}
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
	// Propose the command to the shard's Raft group.
	_, err = s.nodeHost.SyncPropose(ctx, cs, cmdBytes)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose command: %v", err)
	}

	return &worker.DeleteVectorResponse{}, nil
}
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
