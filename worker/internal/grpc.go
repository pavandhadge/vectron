package internal

import (
	"context"
	"log"

	"github.com/pavandhadge/vectron/worker/internal/storage"
	"github.com/pavandhadge/vectron/worker/proto/worker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	log.Printf("Received StoreVector request for ID: %s", req.GetVector().GetId())
	if req.GetVector() == nil {
		return nil, status.Error(codes.InvalidArgument, "vector is nil")
	}
	if req.GetVector().GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "vector ID is empty")
	}
	if len(req.GetVector().GetVector()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "vector data is empty")
	}

	err := s.storage.StoreVector(req.Vector.Id, req.Vector.Vector, req.Vector.Metadata)
	if err != nil {
		log.Printf("Failed to store vector with ID %s: %v", req.GetVector().GetId(), err)
		return nil, status.Errorf(codes.Internal, "failed to store vector: %v", err)
	}
	return &worker.StoreVectorResponse{}, nil
}

// GetVector retrieves a vector.
func (s *GrpcServer) GetVector(ctx context.Context, req *worker.GetVectorRequest) (*worker.GetVectorResponse, error) {
	log.Printf("Received GetVector request for ID: %s", req.GetId())
	vec, meta, err := s.storage.GetVector(req.Id)
	if err != nil {
		log.Printf("Failed to get vector with ID %s: %v", req.GetId(), err)
		return nil, status.Errorf(codes.Internal, "failed to get vector: %v", err)
	}
	if vec == nil {
		return nil, status.Errorf(codes.NotFound, "vector with ID %s not found", req.GetId())
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
	log.Printf("Received DeleteVector request for ID: %s", req.GetId())
	err := s.storage.DeleteVector(req.Id)
	if err != nil {
		log.Printf("Failed to delete vector with ID %s: %v", req.GetId(), err)
		return nil, status.Errorf(codes.Internal, "failed to delete vector: %v", err)
	}
	return &worker.DeleteVectorResponse{}, nil
}

// Search searches for vectors.
func (s *GrpcServer) Search(ctx context.Context, req *worker.SearchRequest) (*worker.SearchResponse, error) {
	log.Printf("Received Search request")
	if len(req.GetVector()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "search vector is empty")
	}
	if req.GetK() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "k must be greater than 0")
	}

	var (
		ids []string
		err error
	)
	if req.BruteForce {
		log.Printf("Performing brute-force search")
		ids, err = s.storage.BruteForceSearch(req.Vector, int(req.K))
	} else {
		log.Printf("Performing HNSW search")
		ids, err = s.storage.Search(req.Vector, int(req.K))
	}

	if err != nil {
		log.Printf("Failed to search: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to search: %v", err)
	}
	return &worker.SearchResponse{Ids: ids}, nil
}

// Put stores a key-value pair.
func (s *GrpcServer) Put(ctx context.Context, req *worker.PutRequest) (*worker.PutResponse, error) {
	log.Printf("Received Put request")
	if req.GetKv() == nil {
		return nil, status.Error(codes.InvalidArgument, "key-value pair is nil")
	}
	err := s.storage.Put(req.Kv.Key, req.Kv.Value)
	if err != nil {
		log.Printf("Failed to put key-value: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to put key-value: %v", err)
	}
	return &worker.PutResponse{}, nil
}

// Get retrieves a value for a key.
func (s *GrpcServer) Get(ctx context.Context, req *worker.GetRequest) (*worker.GetResponse, error) {
	log.Printf("Received Get request")
	val, err := s.storage.Get(req.Key)
	if err != nil {
		log.Printf("Failed to get value: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get value: %v", err)
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
	log.Printf("Received Delete request")
	err := s.storage.Delete(req.Key)
	if err != nil {
		log.Printf("Failed to delete key-value: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to delete key-value: %v", err)
	}
	return &worker.DeleteResponse{}, nil
}

// Status returns the status of the worker.
func (s *GrpcServer) Status(ctx context.Context, req *worker.StatusRequest) (*worker.StatusResponse, error) {
	log.Printf("Received Status request")
	statusMsg, err := s.storage.Status()
	if err != nil {
		log.Printf("Failed to get status: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get status: %v", err)
	}
	return &worker.StatusResponse{Status: statusMsg}, nil
}

// Flush flushes the storage.
func (s *GrpcServer) Flush(ctx context.Context, req *worker.FlushRequest) (*worker.FlushResponse, error) {
	log.Printf("Received Flush request")
	err := s.storage.Flush()
	if err != nil {
		log.Printf("Failed to flush: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to flush: %v", err)
	}
	return &worker.FlushResponse{}, nil
}
