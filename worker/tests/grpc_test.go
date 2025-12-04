package tests

import (
	"context"
	"log"
	"net"
	"os"
	"testing"
	"time"

	// "github.com/pavandhadge/vectron/worker/internal"
	"github.com/pavandhadge/vectron/worker/internal"
	"github.com/pavandhadge/vectron/worker/internal/storage"
	"github.com/pavandhadge/vectron/worker/proto/worker"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func setupTestGRPCServer(t *testing.T) (worker.WorkerClient, func()) {
	// Setup storage
	path := "./test_db_grpc"
	db := storage.NewPebbleDB()
	opts := &storage.Options{
		Path: path,
		HNSWConfig: storage.HNSWConfig{
			Dim:            2,
			M:              16,
			EfConstruction: 200,
			EfSearch:       100,
			DistanceMetric: "euclidean",
			WALEnabled:     false,
		},
	}
	err := db.Init(opts.Path, opts)
	assert.NoError(t, err)

	// Start gRPC server
	grpcServer := internal.NewGrpcServer(db)
	lis, err := net.Listen("tcp", ":0") // Use a random available port
	assert.NoError(t, err)
	s := grpc.NewServer()
	worker.RegisterWorkerServer(s, grpcServer)

	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	// Create gRPC client
	conn, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.NoError(t, err)
	client := worker.NewWorkerClient(conn)

	cleanup := func() {
		s.Stop()
		conn.Close()
		db.Close()
		os.RemoveAll(path)
	}

	return client, cleanup
}

func TestGRPCStoreAndSearch(t *testing.T) {
	client, cleanup := setupTestGRPCServer(t)
	defer cleanup()

	// Store a vector
	storeReq := &worker.StoreVectorRequest{
		Vector: &worker.Vector{
			Id:     "test_vector",
			Vector: []float32{1.0, 2.0},
		},
	}
	_, err := client.StoreVector(context.Background(), storeReq)
	assert.NoError(t, err)

	// Wait for a short time to ensure the vector is indexed
	time.Sleep(100 * time.Millisecond)

	// Search for the vector
	searchReq := &worker.SearchRequest{
		Vector: []float32{1.0, 2.0},
		K:      1,
	}
	searchResp, err := client.Search(context.Background(), searchReq)
	assert.NoError(t, err)
	assert.Len(t, searchResp.GetIds(), 1)
	assert.Equal(t, "test_vector", searchResp.GetIds()[0])
}
