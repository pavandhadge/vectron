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
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pavandhadge/vectron/apigateway/internal/middleware"
	"github.com/pavandhadge/vectron/apigateway/internal/translator"
	pb "github.com/pavandhadge/vectron/apigateway/proto/apigateway"
	placementpb "github.com/pavandhadge/vectron/apigateway/proto/placementdriver"
	workerpb "github.com/pavandhadge/vectron/apigateway/proto/worker"

	authpb "github.com/pavandhadge/vectron/auth/service/proto/auth"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

var cfg = LoadConfig()

type LeaderInfo struct {
	client placementpb.PlacementServiceClient
	conn   *grpc.ClientConn
}

// gatewayServer implements the public gRPC VectronService. It acts as a facade,
// forwarding requests to the appropriate backend services (placement driver or workers).
type gatewayServer struct {
	pb.UnimplementedVectronServiceServer
	pdAddrs  []string
	leader   *LeaderInfo
	leaderMu sync.RWMutex
}

func (s *gatewayServer) getPlacementClient() (placementpb.PlacementServiceClient, error) {
	s.leaderMu.RLock()
	if s.leader != nil && s.leader.client != nil {
		s.leaderMu.RUnlock()
		return s.leader.client, nil
	}
	s.leaderMu.RUnlock()
	return s.updateLeader()
}

func (s *gatewayServer) updateLeader() (placementpb.PlacementServiceClient, error) {
	s.leaderMu.Lock()
	defer s.leaderMu.Unlock()

	// Close existing connection if any
	if s.leader != nil && s.leader.conn != nil {
		s.leader.conn.Close()
	}

	for _, addr := range s.pdAddrs {
		conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock(), grpc.WithTimeout(2*time.Second))
		if err != nil {
			log.Printf("Failed to connect to PD node %s: %v", addr, err)
			continue
		}

		client := placementpb.NewPlacementServiceClient(conn)
		// Use ListCollections as a way to check for leadership.
		_, err = client.ListCollections(context.Background(), &placementpb.ListCollectionsRequest{})
		if err != nil {
			conn.Close()
			log.Printf("Failed to get leader from PD node %s: %v", addr, err)
			continue
		}

		log.Println("Connected to new PD leader at", conn.Target())
		s.leader = &LeaderInfo{client: client, conn: conn}
		return client, nil
	}

	return nil, status.Error(codes.Unavailable, "no placement driver leader found")
}

// forwardToWorker is a helper function that encapsulates the logic for service discovery and request forwarding.
// It queries the placement driver to find the correct worker for a given collection and vector ID,
// establishes a connection, and then executes a callback function with the worker client.
func (s *gatewayServer) forwardToWorker(ctx context.Context, collection string, vectorID string, call func(workerpb.WorkerServiceClient, uint64) (interface{}, error)) (interface{}, error) {
	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}
	// 1. Ask the placement driver for the worker address.
	resp, err := placementClient.GetWorker(ctx, &placementpb.GetWorkerRequest{
		Collection: collection,
		VectorId:   vectorID,
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			resp, err = placementClient.GetWorker(ctx, &placementpb.GetWorkerRequest{
				Collection: collection,
				VectorId:   vectorID,
			})
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not get worker for collection %q after leader update: %v", collection, err)
			}
		} else {
			return nil, status.Errorf(codes.Internal, "could not get worker for collection %q: %v", collection, err)
		}
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

	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}

	// This is a metadata operation, so it goes to the placement driver.
	pdReq := &placementpb.CreateCollectionRequest{
		Name:      req.Name,
		Dimension: req.Dimension,
		Distance:  req.Distance,
	}
	res, err := placementClient.CreateCollection(ctx, pdReq)
	if err != nil {
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			res, err = placementClient.CreateCollection(ctx, pdReq)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
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
	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}
	pdReq := &placementpb.ListCollectionsRequest{}
	res, err := placementClient.ListCollections(ctx, pdReq)
	if err != nil {
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			res, err = placementClient.ListCollections(ctx, pdReq)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return &pb.ListCollectionsResponse{Collections: res.Collections}, nil
}

// GetCollectionStatus handles the RPC for getting the status of a collection.
// This is a metadata operation, so it goes to the placement driver.
func (s *gatewayServer) GetCollectionStatus(ctx context.Context, req *pb.GetCollectionStatusRequest) (*pb.GetCollectionStatusResponse, error) {
	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}
	pdReq := &placementpb.GetCollectionStatusRequest{
		Name: req.Name,
	}
	res, err := placementClient.GetCollectionStatus(ctx, pdReq)
	if err != nil {
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			res, err = placementClient.GetCollectionStatus(ctx, pdReq)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	shardStatuses := make([]*pb.ShardStatus, 0, len(res.Shards))
	for _, shard := range res.Shards {
		shardStatuses = append(shardStatuses, &pb.ShardStatus{
			ShardId:  shard.ShardId,
			Replicas: shard.Replicas,
			LeaderId: shard.LeaderId,
			Ready:    shard.Ready,
		})
	}

	return &pb.GetCollectionStatusResponse{
		Name:      res.Name,
		Dimension: res.Dimension,
		Distance:  res.Distance,
		Shards:    shardStatuses,
	}, nil
}

// Start initializes and runs the API Gateway's gRPC and HTTP servers.
// It can optionally take a pre-configured net.Listener for the gRPC server,
// useful for testing scenarios. It returns the gRPC server instance and the
// client connection to the auth service.
func Start(config Config, grpcListener net.Listener) (*grpc.Server, *grpc.ClientConn) {
	// Create the gateway server with the list of PD addresses
	server := &gatewayServer{
		pdAddrs: strings.Split(config.PlacementDriver, ","),
	}
	// Initialize the leader connection with retry logic
	maxRetries := 5
	baseBackoff := 1 * time.Second
	for i := 0; i < maxRetries; i++ {
		if _, err := server.updateLeader(); err == nil {
			// Successfully connected to a leader
			break
		}
		if i == maxRetries-1 {
			log.Fatalf("Failed to initialize connection with placement driver leader after %d attempts", maxRetries)
		}
		wait := baseBackoff * time.Duration(1<<i)
		log.Printf("Failed to connect to placement driver leader, retrying in %v...", wait)
		time.Sleep(wait)
	}

	// Establish gRPC connection to Auth service
	authConn, err := grpc.Dial(config.AuthServiceAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to Auth service: %v", err)
	}
	authClient := authpb.NewAuthServiceClient(authConn)

	// Create the gRPC server with a chain of unary interceptors for middleware.
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			middleware.AuthInterceptor(authClient),            // Handles API Key authentication.
			middleware.LoggingInterceptor,                     // Logs incoming requests.
			middleware.RateLimitInterceptor(config.RateLimitRPS), // Enforces rate limiting.
		),
	)
	pb.RegisterVectronServiceServer(grpcServer, server)

	// Set up the HTTP/JSON gateway to proxy requests to the gRPC server.
	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := pb.RegisterVectronServiceHandlerFromEndpoint(context.Background(), mux, config.GRPCAddr, opts); err != nil {
		log.Fatalf("Failed to register VectronServiceHandlerFromEndpoint: %v", err)
	}

	// Start the gRPC server in a separate goroutine.
	go func() {
		var lis net.Listener
		if grpcListener != nil {
			lis = grpcListener
		} else {
			var err error
			log.Printf("DEBUG: API Gateway config.GRPCAddr: %s, GRPC_ADDR env: %s", config.GRPCAddr, os.Getenv("GRPC_ADDR"))
			lis, err = net.Listen("tcp", config.GRPCAddr)
			if err != nil {
				log.Fatalf("Failed to listen for gRPC server: %v", err)
			}
		}
		log.Printf("Vectron gRPC API (SDKs)     → %s", lis.Addr().String())
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC server failed to serve: %v", err)
		}
	}()

	// Start the HTTP server in a separate goroutine.
	go func() {
		log.Printf("Vectron HTTP API (curl)      → %s", config.HTTPAddr)
		log.Printf("Using placement driver           → %s", config.PlacementDriver)
		if err := http.ListenAndServe(config.HTTPAddr, mux); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed to serve: %v", err)
		}
	}()

	return grpcServer, authConn
}

// ================ MAIN ================

func main() {
	// Start the servers.
	// In the main function, we pass nil for grpcListener to let Start create its own.
	_, authConn := Start(cfg, nil)
	defer authConn.Close()

	// Block forever to keep the services running.
	select {}
}
