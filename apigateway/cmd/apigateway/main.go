// apigateway/main.go — FINAL VERSION
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

type gatewayServer struct {
	pb.UnimplementedVectronServiceServer
	placementClient placementpb.PlacementServiceClient
}

// forwardToWorker gets a worker for a given collection and optional vector ID,
// connects to it, and executes the given function.
func (s *gatewayServer) forwardToWorker(ctx context.Context, collection string, vectorID string, call func(workerpb.WorkerServiceClient, uint64) (interface{}, error)) (interface{}, error) {
	resp, err := s.placementClient.GetWorker(ctx, &placementpb.GetWorkerRequest{
		Collection: collection,
		VectorId:   vectorID,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get worker for collection %q: %v", collection, err)
	}
	if resp.Address == "" {
		return nil, status.Errorf(codes.NotFound, "collection %q not found", collection)
	}

	conn, err := grpc.Dial(resp.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "worker for collection %q at %s is unreachable: %v", collection, resp.Address, err)
	}
	defer conn.Close()

	client := workerpb.NewWorkerServiceClient(conn)
	return call(client, uint64(resp.ShardId))
}

// ================ RPCs IMPLEMENTED ================

func (s *gatewayServer) CreateCollection(ctx context.Context, req *pb.CreateCollectionRequest) (*pb.CreateCollectionResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name cannot be empty")
	}
	if req.Dimension <= 0 {
		return nil, status.Error(codes.InvalidArgument, "dimension must be a positive number")
	}

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

		// Get worker for this specific point.
		_, err := s.forwardToWorker(ctx, req.Collection, point.Id, func(client workerpb.WorkerServiceClient, shardID uint64) (interface{}, error) {
			workerReq := translator.ToWorkerStoreVectorRequestFromPoint(point, shardID)
			_, err := client.StoreVector(ctx, workerReq)
			return nil, err
		})

		if err != nil {
			// In a real implementation, we might want to collect errors
			// and continue, or implement rollback logic.
			// For now, fail on the first error.
			return nil, status.Errorf(codes.Internal, "failed to upsert point %s: %v", point.Id, err)
		}
		upsertedCount++
	}

	return &pb.UpsertResponse{Upserted: upsertedCount}, nil
}

func (s *gatewayServer) Search(ctx context.Context, req *pb.SearchRequest) (*pb.SearchResponse, error) {
	if req.Collection == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name cannot be empty")
	}
	if len(req.Vector) == 0 {
		return nil, status.Error(codes.InvalidArgument, "search vector cannot be empty")
	}

	if req.TopK == 0 {
		req.TopK = 10
	}
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

func (s *gatewayServer) ListCollections(ctx context.Context, req *pb.ListCollectionsRequest) (*pb.ListCollectionsResponse, error) {
	pdReq := &placementpb.ListCollectionsRequest{}
	res, err := s.placementClient.ListCollections(ctx, pdReq)
	if err != nil {
		return nil, err
	}
	return &pb.ListCollectionsResponse{Collections: res.Collections}, nil
}

func Start(grpcAddr, httpAddr, placementDriverAddr string) {
	// Connect to placement driver
	conn, err := grpc.Dial(placementDriverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("Failed to connect to placement driver:", err)
	}
	defer conn.Close()
	placementClient := placementpb.NewPlacementServiceClient(conn)

	// gRPC Server
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			middleware.LoggingInterceptor,
			middleware.RateLimitInterceptor(cfg.RateLimitRPS),
			middleware.AuthInterceptor,
		),
	)
	pb.RegisterVectronServiceServer(grpcServer, &gatewayServer{placementClient: placementClient})

	// HTTP Gateway
	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := pb.RegisterVectronServiceHandlerFromEndpoint(context.Background(), mux, grpcAddr, opts); err != nil {
		log.Fatal(err)
	}

	// Start both
	go func() {
		lis, _ := net.Listen("tcp", grpcAddr)
		log.Printf("Vectron gRPC API (SDKs)     → %s", grpcAddr)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatal(err)
		}
	}()

	log.Printf("Vectron HTTP API (curl)      → %s", httpAddr)
	log.Printf("Using placement driver           → %s", placementDriverAddr)
	if err := http.ListenAndServe(httpAddr, mux); err != nil {
		log.Fatal(err)
	}
}

// ================ MAIN ================

func main() {
	middleware.SetJWTSecret(cfg.JWTSecret)
	Start(cfg.GRPCAddr, cfg.HTTPAddr, cfg.PlacementDriver)
}
