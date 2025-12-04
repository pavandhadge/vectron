package main

import (
	"flag"
	"fmt"
	"log"
	"net"

	"github.com/pavandhadge/vectron/worker/internal"
	"github.com/pavandhadge/vectron/worker/internal/storage"
	"github.com/pavandhadge/vectron/worker/proto/worker"
	"google.golang.org/grpc"
)

func main() {
	var (
		grpcAddr    = flag.String("grpc-addr", ":9090", "gRPC server address")
		storagePath = flag.String("storage-path", "./data", "Storage path")
	)
	flag.Parse()

	// Initialize storage
	db := storage.NewPebbleDB()
	opts := &storage.Options{
		Path: *storagePath,
		HNSWConfig: storage.HNSWConfig{
			WALEnabled: true,
		},
	}
	if err := db.Init(opts.Path, opts); err != nil {
		log.Fatalf("failed to initialize storage: %v", err)
	}
	defer db.Close()

	// Create gRPC server
	grpcServer := internal.NewGrpcServer(db)

	// Start gRPC server
	lis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	worker.RegisterWorkerServer(s, grpcServer)
	fmt.Printf("gRPC server listening on %s\n", *grpcAddr)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
