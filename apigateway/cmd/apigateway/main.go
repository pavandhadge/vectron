// This file is the main entry point for the API Gateway service.
// It sets up and runs the gRPC server and the HTTP/JSON gateway,
// which exposes the public Vectron API to clients. It handles request
// forwarding to the appropriate worker nodes after consulting the placement driver.

package main

import (
	"context"
	"log"
	"net"
	"net/http"

	"github.com/pavandhadge/vectron/apigateway/internal/middleware"
	"github.com/pavandhadge/vectron/apigateway/internal/translator"
	pb "github.com/pavandhadge/vectron/apigateway/proto/apigateway"
	placementpb "github.com/pavandhadge/vectron/apigateway/proto/placementdriver"
	workerpb "github.com/pavandhadge/vectron/apigateway/proto/worker"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

var cfg = LoadConfig()

// gatewayServer implements the public gRPC VectronService. It acts as a facade,
// forwarding requests to the appropriate backend services (placement driver or workers).
type gatewayServer struct {
	pb.UnimplementedVectronServiceServer
	placementClient placementpb.PlacementServiceClient
}

// forwardToWorker is a helper function that encapsulates the logic for service discovery and request forwarding.
// It queries the placement driver to find the correct worker for a given collection and vector ID,
// establishes a connection, and then executes a callback function with the worker client.
func (s *gatewayServer) forwardToWorker(ctx context.Context, collection string, vectorID string, call func(workerpb.WorkerServiceClient, uint64) (interface{}, error)) (interface{}, error) {
	// 1. Ask the placement driver for the worker address.
	resp, err := s.placementClient.GetWorker(ctx, &placementpb.GetWorkerRequest{
		Collection: collection,
		VectorId:   vectorID,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get worker for collection %q: %v", collection, err)
	}
	if resp.GetGrpcAddress() == "" {
		return nil, status.Errorf(codes.NotFound, "collection %q not found", collection)
	}

	// 2. Connect to the worker.
	conn, err := grpc.Dial(resp.GetGrpcAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "worker for collection %q at %s is unreachable: %v", collection, resp.GetGrpcAddress(), err)
	}
	defer conn.Close()

	// 3. Execute the call on the worker.
	client := workerpb.NewWorkerServiceClient(conn)
	return call(client, uint64(resp.ShardId))
}

// ================ RPCs IMPLEMENTED ================

// CreateCollection handles the RPC for creating a new collection.
// It validates the request and forwards it to the placement driver.
func (s *gatewayServer) CreateCollection(ctx context.Context, req *pb.CreateCollectionRequest) (*pb.CreateCollectionResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name cannot be empty")
	}
	if req.Dimension <= 0 {
		return nil, status.Error(codes.InvalidArgument, "dimension must be a positive number")
	}

	// This is a metadata operation, so it goes to the placement driver.
	pdReq := &placementpb.CreateCollectionRequest{
		Name:      req.Name,
		Dimension: req.Dimension,
		Distance:  req.Distance,
	}
	res, err := s.placementClient.CreateCollection(ctx, pdReq)
	if err != nil {
		return nil, err
	}
	return &pb.CreateCollectionResponse{Success: res.Success}, nil
}

// Upsert handles the RPC for upserting points into a collection.
// It iterates through the points and forwards each one to the appropriate worker.
func (s *gatewayServer) Upsert(ctx context.Context, req *pb.UpsertRequest) (*pb.UpsertResponse, error) {
	if req.Collection == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name cannot be empty")
	}
	if len(req.Points) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one point is required for upsert")
	}

	var upsertedCount int32
	for _, point := range req.Points {
		if point.Id == "" {
			return nil, status.Error(codes.InvalidArgument, "point ID cannot be empty")
		}
		if len(point.Vector) == 0 {
			return nil, status.Error(codes.InvalidArgument, "point vector cannot be empty")
		}

		// Get worker for this specific point and forward the request.
		_, err := s.forwardToWorker(ctx, req.Collection, point.Id, func(client workerpb.WorkerServiceClient, shardID uint64) (interface{}, error) {
			workerReq := translator.ToWorkerStoreVectorRequestFromPoint(point, shardID)
			_, err := client.StoreVector(ctx, workerReq)
			return nil, err
		})

		if err != nil {
			// In a real implementation, we might want to collect errors
			// and continue, or implement rollback logic.
			// For now, we fail on the first error.
			return nil, status.Errorf(codes.Internal, "failed to upsert point %s: %v", point.Id, err)
		}
		upsertedCount++
	}

	return &pb.UpsertResponse{Upserted: upsertedCount}, nil
}

// Search handles the RPC for searching for similar vectors.
// It forwards the search request to a relevant worker.
func (s *gatewayServer) Search(ctx context.Context, req *pb.SearchRequest) (*pb.SearchResponse, error) {
	if req.Collection == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name cannot be empty")
	}
	if len(req.Vector) == 0 {
		return nil, status.Error(codes.InvalidArgument, "search vector cannot be empty")
	}

	if req.TopK == 0 {
		req.TopK = 10 // Default to 10 nearest neighbors
	}

	// For search, we can query any worker that has a replica of the shard.
	// The vectorID is empty, letting the placement driver pick a suitable worker.
	result, err := s.forwardToWorker(ctx, req.Collection, "", func(c workerpb.WorkerServiceClient, shardID uint64) (interface{}, error) {
		workerReq := translator.ToWorkerSearchRequest(req, shardID)
		res, err := c.Search(ctx, workerReq)
		if err != nil {
			return nil, err
		}
		return translator.FromWorkerSearchResponse(res), nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*pb.SearchResponse), nil
}

// Get handles the RPC for retrieving a point by its ID.
func (s *gatewayServer) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	result, err := s.forwardToWorker(ctx, req.Collection, req.Id, func(c workerpb.WorkerServiceClient, shardID uint64) (interface{}, error) {
		workerReq := translator.ToWorkerGetVectorRequest(req, shardID)
		res, err := c.GetVector(ctx, workerReq)
		if err != nil {
			return nil, err
		}
		return translator.FromWorkerGetVectorResponse(res), nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*pb.GetResponse), nil
}

// Delete handles the RPC for deleting a point by its ID.
func (s *gatewayServer) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	result, err := s.forwardToWorker(ctx, req.Collection, req.Id, func(c workerpb.WorkerServiceClient, shardID uint64) (interface{}, error) {
		workerReq := translator.ToWorkerDeleteVectorRequest(req, shardID)
		res, err := c.DeleteVector(ctx, workerReq)
		if err != nil {
			return nil, err
		}
		return translator.FromWorkerDeleteVectorResponse(res), nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*pb.DeleteResponse), nil
}

// ListCollections handles the RPC for listing all collections.
// This is a metadata operation, so it goes to the placement driver.
func (s *gatewayServer) ListCollections(ctx context.Context, req *pb.ListCollectionsRequest) (*pb.ListCollectionsResponse, error) {
	pdReq := &placementpb.ListCollectionsRequest{}
	res, err := s.placementClient.ListCollections(ctx, pdReq)
	if err != nil {
		return nil, err
	}
	return &pb.ListCollectionsResponse{Collections: res.Collections}, nil
}

// Start initializes and runs the API Gateway's gRPC and HTTP servers.
func Start(grpcAddr, httpAddr, placementDriverAddr string) {
	// Connect to the placement driver, which is essential for service discovery.
	conn, err := grpc.Dial(placementDriverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("Failed to connect to placement driver:", err)
	}
	defer conn.Close()
	placementClient := placementpb.NewPlacementServiceClient(conn)

	// Create the gRPC server with a chain of unary interceptors for middleware.
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			middleware.AuthInterceptor,                        // Handles JWT authentication.
			middleware.LoggingInterceptor,                     // Logs incoming requests.
			middleware.RateLimitInterceptor(cfg.RateLimitRPS), // Enforces rate limiting.
		),
	)
	pb.RegisterVectronServiceServer(grpcServer, &gatewayServer{placementClient: placementClient})

	// Set up the HTTP/JSON gateway to proxy requests to the gRPC server.
	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := pb.RegisterVectronServiceHandlerFromEndpoint(context.Background(), mux, grpcAddr, opts); err != nil {
		log.Fatal(err)
	}

	// Start the gRPC server in a separate goroutine.
	go func() {
		lis, _ := net.Listen("tcp", grpcAddr)
		log.Printf("Vectron gRPC API (SDKs)     → %s", grpcAddr)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatal(err)
		}
	}()

	// Start the HTTP server in the main goroutine.
	log.Printf("Vectron HTTP API (curl)      → %s", httpAddr)
	log.Printf("Using placement driver           → %s", placementDriverAddr)
	if err := http.ListenAndServe(httpAddr, mux); err != nil {
		log.Fatal(err)
	}
}

// ================ MAIN ================

func main() {
	// Load configuration and set up middleware.
	middleware.SetJWTSecret(cfg.JWTSecret)
	// Start the servers.
	Start(cfg.GRPCAddr, cfg.HTTPAddr, cfg.PlacementDriver)
}
