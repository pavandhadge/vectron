package internal

import (
	"context"
	"fmt"

	"github.com/pavandhadge/vectron/internal/proto/worker"
	"github.com/pavandhadge/vectron/worker/internal/storage"
)

// GrpcServer is the gRPC server for the worker.
type GrpcServer struct {
	worker.UnimplementedWorkerServer
	storage *storage.PebbleDB
}

// NewGrpcServer creates a new instance of the gRPC server.
func NewGrpcServer(storage *storage.PebbleDB) *GrpcServer {
	return &GrpcServer{
		storage: storage,
	}
}

// StoreVector stores a vector.
func (s *GrpcServer) StoreVector(ctx context.Context, req *worker.StoreVectorRequest) (*worker.StoreVectorResponse, error) {
	if req.Vector == nil {
		return nil, fmt.Errorf("vector is nil")
	}
	err := s.storage.StoreVector(req.Vector.Id, req.Vector.Vector, req.Vector.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to store vector: %w", err)
	}
	return &worker.StoreVectorResponse{}, nil
}

// GetVector retrieves a vector.
func (s *GrpcServer) GetVector(ctx context.Context, req *worker.GetVectorRequest) (*worker.GetVectorResponse, error) {
	vec, meta, err := s.storage.GetVector(req.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to get vector: %w", err)
	}
	return &worker.GetVectorResponse{
		Vector: &worker.Vector{
			Id:       req.Id,
			Vector:   vec,
			Metadata: meta,
		},
	}, nil
}

// DeleteVector deletes a vector.
func (s *GrpcServer) DeleteVector(ctx context.Context, req *worker.DeleteVectorRequest) (*worker.DeleteVectorResponse, error) {
	err := s.storage.DeleteVector(req.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to delete vector: %w", err)
	}
	return &worker.DeleteVectorResponse{}, nil
}

// Search searches for vectors.
func (s *GrpcServer) Search(ctx context.Context, req *worker.SearchRequest) (*worker.SearchResponse, error) {
	ids, err := s.storage.Search(req.Vector, int(req.K))
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	return &worker.SearchResponse{Ids: ids}, nil
}

// Put stores a key-value pair.
func (s *GrpcServer) Put(ctx context.Context, req *worker.PutRequest) (*worker.PutResponse, error) {
	if req.Kv == nil {
		return nil, fmt.Errorf("key-value pair is nil")
	}
	err := s.storage.Put(req.Kv.Key, req.Kv.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to put key-value: %w", err)
	}
	return &worker.PutResponse{}, nil
}

// Get retrieves a value for a key.
func (s *GrpcServer) Get(ctx context.Context, req *worker.GetRequest) (*worker.GetResponse, error) {
	val, err := s.storage.Get(req.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to get value: %w", err)
	}
	return &worker.GetResponse{
		Kv: &worker.KeyValuePair{
			Key:   req.Key,
			Value: val,
		},
	}, nil
}

// Delete deletes a key-value pair.
func (s *GrpcServer) Delete(ctx context.Context, req *worker.DeleteRequest) (*worker.DeleteResponse, error) {
	err := s.storage.Delete(req.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to delete key-value: %w", err)
	}
	return &worker.DeleteResponse{}, nil
}

// Status returns the status of the worker.
func (s *GrpcServer) Status(ctx context.Context, req *worker.StatusRequest) (*worker.StatusResponse, error) {
	status, err := s.storage.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}
	return &worker.StatusResponse{Status: status}, nil
}

// Flush flushes the storage.
func (s *GrpcServer) Flush(ctx context.Context, req *worker.FlushRequest) (*worker.FlushResponse, error) {
	err := s.storage.Flush()
	if err != nil {
		return nil, fmt.Errorf("failed to flush: %w", err)
	}
	return &worker.FlushResponse{}, nil
}
