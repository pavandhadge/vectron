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
	workerpb "github.com/pavandhadge/vectron/worker/proto/worker"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

var cfg = LoadConfig()

type gatewayServer struct {
	pb.UnimplementedVectronServiceServer
	placementClient placementpb.PlacementServiceClient
}

// Generic forwarder — used by all RPCs that go to a worker
func (s *gatewayServer) forwardToWorker(ctx context.Context, collection string, call func(workerpb.WorkerServiceClient) (interface{}, error)) (interface{}, error) {
	resp, err := s.placementClient.GetWorker(ctx, &placementpb.GetWorkerRequest{
		Collection: collection,
	})
	if err != nil || resp.Address == "" {
		return nil, status.Errorf(grpc.Code(err), "collection %s not found", collection)
	}

	conn, err := grpc.Dial(resp.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, status.Errorf(grpc.Code(err), "worker unreachable")
	}
	defer conn.Close()

	client := workerpb.NewWorkerServiceClient(conn)
	return call(client)
}

// ================ RPCs IMPLEMENTED ================

func (s *gatewayServer) CreateCollection(ctx context.Context, req *pb.CreateCollectionRequest) (*pb.CreateCollectionResponse, error) {
	// This will be a call to the placement driver
	// For now, we'll return a dummy response.
	return &pb.CreateCollectionResponse{Success: true}, nil
}

func (s *gatewayServer) Upsert(ctx context.T, req *pb.UpsertRequest) (*pb.UpsertResponse, error) {
	result, err := s.forwardToWorker(ctx, req.Collection, func(c workerpb.WorkerServiceClient) (interface{}, error) {
		workerReq := translator.ToWorkerStoreVectorRequest(req)
		res, err := c.StoreVector(ctx, workerReq)
		if err != nil {
			return nil, err
		}
		return translator.FromWorkerStoreVectorResponse(res), nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*pb.UpsertResponse), nil
}

func (s *gatewayServer) Search(ctx context.Context, req *pb.SearchRequest) (*pb.SearchResponse, error) {
	if req.TopK == 0 {
		req.TopK = 10
	}
	result, err := s.forwardToWorker(ctx, req.Collection, func(c workerpb.WorkerServiceClient) (interface{}, error) {
		workerReq := translator.ToWorkerSearchRequest(req)
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
	result, err := s.forwardToWorker(ctx, req.Collection, func(c workerpb.WorkerServiceClient) (interface{}, error) {
		workerReq := translator.ToWorkerGetVectorRequest(req)
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
	result, err := s.forwardToWorker(ctx, req.Collection, func(c workerpb.WorkerServiceClient) (interface{}, error) {
		workerReq := translator.ToWorkerDeleteVectorRequest(req)
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
	// This will be a call to the placement driver
	// For now, we'll return a dummy response.
	return &pb.ListCollectionsResponse{Collections: []string{"dummy_collection"}}, nil
}

// ================ MAIN ================

func main() {
	middleware.SetJWTSecret(cfg.JWTSecret)

	// Connect to placement driver
	conn, err := grpc.Dial(cfg.PlacementDriver, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("Failed to connect to placement driver:", err)
	}
	defer conn.Close()
	placementClient := placementpb.NewPlacementServiceClient(conn)

	// gRPC Server — primary path (fast SDKs)
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			middleware.LoggingInterceptor,
			middleware.RateLimitInterceptor(cfg.RateLimitRPS),
			middleware.AuthInterceptor,
		),
	)
	pb.RegisterVectronServiceServer(grpcServer, &gatewayServer{placementClient: placementClient})

	// HTTP Gateway — secondary path (curl, browser)
	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := pb.RegisterVectronServiceHandlerFromEndpoint(context.Background(), mux, cfg.GRPCAddr, opts); err != nil {
		log.Fatal(err)
	}

	// Start both
	go func() {
		lis, _ := net.Listen("tcp", cfg.GRPCAddr)
		log.Printf("Vectron gRPC API (SDKs)     → %s", cfg.GRPCAddr)
		log.Fatal(grpcServer.Serve(lis))
	}()

	log.Printf("Vectron HTTP API (curl)      → %s", cfg.HTTPAddr)
	log.Printf("Using placement driver           → %s", cfg.PlacementDriver)
	log.Fatal(http.ListenAndServe(cfg.HTTPAddr, mux))
}
