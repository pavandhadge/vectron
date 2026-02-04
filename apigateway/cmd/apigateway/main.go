// This file is the main entry point for the API Gateway service.
// It sets up and runs the gRPC server and the HTTP/JSON gateway,
// which exposes the public Vectron API to clients. It handles request
// forwarding to the appropriate backend services (placement driver or workers).

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pavandhadge/vectron/apigateway/internal/feedback"
	"github.com/pavandhadge/vectron/apigateway/internal/management"
	"github.com/pavandhadge/vectron/apigateway/internal/middleware"
	"github.com/pavandhadge/vectron/apigateway/internal/translator"
	pb "github.com/pavandhadge/vectron/shared/proto/apigateway"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
	placementpb "github.com/pavandhadge/vectron/shared/proto/placementdriver"
	reranker "github.com/pavandhadge/vectron/shared/proto/reranker"
	workerpb "github.com/pavandhadge/vectron/shared/proto/worker"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
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
	pdAddrs         []string
	leader          *LeaderInfo
	leaderMu        sync.RWMutex
	authClient      authpb.AuthServiceClient
	rerankerClient  reranker.RerankServiceClient
	feedbackService *feedback.Service
	managementMgr   *management.Manager
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

func (s *gatewayServer) UpdateUserProfile(ctx context.Context, req *pb.UpdateUserProfileRequest) (*pb.UpdateUserProfileResponse, error) {
	authReq := &authpb.UpdateUserProfileRequest{
		Plan: req.Plan,
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Errorf(codes.Unauthenticated, "missing metadata")
	}
	authHeader := md.Get("authorization")
	if len(authHeader) == 0 {
		return nil, status.Errorf(codes.Unauthenticated, "missing authorization header")
	}

	authCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", authHeader[0])

	authResp, err := s.authClient.UpdateUserProfile(authCtx, authReq)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update user profile: %v", err)
	}

	return &pb.UpdateUserProfileResponse{
		User: authResp.User,
	}, nil
}

func (s *gatewayServer) SubmitFeedback(ctx context.Context, req *pb.SubmitFeedbackRequest) (*pb.SubmitFeedbackResponse, error) {
	if req.Collection == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name cannot be empty")
	}
	if len(req.FeedbackItems) == 0 {
		return nil, status.Error(codes.InvalidArgument, "feedback items cannot be empty")
	}

	feedbackItems := make([]feedback.FeedbackItem, len(req.FeedbackItems))
	for i, item := range req.FeedbackItems {
		if item.RelevanceScore < 1 || item.RelevanceScore > 5 {
			return nil, status.Error(codes.InvalidArgument, "relevance score must be between 1 and 5")
		}
		feedbackItems[i] = feedback.FeedbackItem{
			ResultID:       item.ResultId,
			RelevanceScore: item.RelevanceScore,
			Clicked:        item.Clicked,
			Position:       item.Position,
			Comment:        item.Comment,
		}
	}

	session := &feedback.FeedbackSession{
		Collection: req.Collection,
		Query:      req.Query,
		Items:      feedbackItems,
		Context:    req.Context,
	}

	if userID, ok := req.Context["user_id"]; ok {
		session.UserID = userID
	}
	if sessionID, ok := req.Context["session_id"]; ok {
		session.SessionID = sessionID
	}

	feedbackID, err := s.feedbackService.StoreFeedback(ctx, session)
	if err != nil {
		log.Printf("Failed to store feedback: %v", err)
		return nil, status.Error(codes.Internal, "failed to store feedback")
	}

	log.Printf("Stored feedback for collection %s: %s", req.Collection, feedbackID)

	return &pb.SubmitFeedbackResponse{
		Success:    true,
		FeedbackId: feedbackID,
	}, nil
}

func (s *gatewayServer) updateLeader() (placementpb.PlacementServiceClient, error) {
	s.leaderMu.Lock()
	defer s.leaderMu.Unlock()

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

func (s *gatewayServer) forwardToWorker(ctx context.Context, collection string, vectorID string, call func(workerpb.WorkerServiceClient, uint64) (interface{}, error)) (interface{}, error) {
	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}

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

	conn, err := grpc.Dial(resp.GetGrpcAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "worker for collection %q at %s is unreachable: %v", collection, resp.GetGrpcAddress(), err)
	}
	defer conn.Close()

	client := workerpb.NewWorkerServiceClient(conn)
	return call(client, uint64(resp.ShardId))
}

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

func (s *gatewayServer) DeleteCollection(ctx context.Context, req *pb.DeleteCollectionRequest) (*pb.DeleteCollectionResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name cannot be empty")
	}

	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}

	pdReq := &placementpb.DeleteCollectionRequest{
		Name: req.Name,
	}

	res, err := placementClient.DeleteCollection(ctx, pdReq)
	if err != nil {
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			res, err = placementClient.DeleteCollection(ctx, pdReq)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return &pb.DeleteCollectionResponse{Success: res.Success}, nil
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

		_, err := s.forwardToWorker(ctx, req.Collection, point.Id, func(client workerpb.WorkerServiceClient, shardID uint64) (interface{}, error) {
			workerReq := translator.ToWorkerStoreVectorRequestFromPoint(point, shardID)
			_, err := client.StoreVector(ctx, workerReq)
			return nil, err
		})

		if err != nil {
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

	workerTopK := req.TopK * 2
	if workerTopK > 100 {
		workerTopK = 100
	}

	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}

	workerListResp, err := placementClient.ListWorkersForCollection(ctx, &placementpb.ListWorkersForCollectionRequest{
		Collection: req.Collection,
	})
	if err != nil {
		// Attempt to update leader and retry if the placement driver client is stale
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			workerListResp, err = placementClient.ListWorkersForCollection(ctx, &placementpb.ListWorkersForCollectionRequest{
				Collection: req.Collection,
			})
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not list workers for collection %q after leader update: %v", req.Collection, err)
			}
		} else {
			return nil, status.Errorf(codes.Internal, "could not list workers for collection %q: %v", req.Collection, err)
		}
	}

	workerAddresses := workerListResp.GetGrpcAddresses()
	if len(workerAddresses) == 0 {
		return &pb.SearchResponse{}, nil // No workers, no results
	}

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		allResults []*pb.SearchResult
		errs      []error
	)

	for _, addr := range workerAddresses {
		wg.Add(1)
		go func(workerAddr string) {
			defer wg.Done()

			conn, err := grpc.Dial(workerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("worker %s unreachable: %v", workerAddr, err))
				mu.Unlock()
				return
			}
			defer conn.Close()

			workerClient := workerpb.NewWorkerServiceClient(conn)
			workerReq := translator.ToWorkerSearchRequest(req, 0) // ShardID 0 for broadcast search
			workerReq.K = int32(workerTopK)

			res, err := workerClient.Search(ctx, workerReq)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("worker %s search failed: %v", workerAddr, err))
				mu.Unlock()
				return
			}

			mu.Lock()
			workerSearchResponse := translator.FromWorkerSearchResponse(res)
			allResults = append(allResults, workerSearchResponse.Results...)
			mu.Unlock()
		}(addr)
	}
	wg.Wait()

	if len(errs) > 0 {
		// For now, return the first error. In a production system,
		// you might want to log all errors and potentially return partial results.
		return nil, status.Errorf(codes.Internal, "failed to search all workers: %v", errs[0])
	}

	searchResponse := &pb.SearchResponse{
		Results: allResults,
	}

	if len(searchResponse.Results) == 0 {
		return searchResponse, nil
	}

	candidates := make([]*reranker.Candidate, len(searchResponse.Results))
	for i, result := range searchResponse.Results {
		metadata := make(map[string]string)
		if result.Payload != nil {
			for k, v := range result.Payload {
				metadata[k] = v
			}
		}

		candidates[i] = &reranker.Candidate{
			Id:       result.Id,
			Score:    result.Score,
			Metadata: metadata,
		}
	}

	rerankReq := &reranker.RerankRequest{
		Query:      req.Query, // Pass the query from the API Gateway's SearchRequest
		Candidates: candidates,
		TopN:       int32(req.TopK),
	}

	rerankResp, err := s.rerankerClient.Rerank(ctx, rerankReq)
	if err != nil {
		log.Println("Reranking failed, returning original results:", err)
		// Sort original results by score in descending order before returning
		sort.Slice(searchResponse.Results, func(i, j int) bool {
			return searchResponse.Results[i].Score > searchResponse.Results[j].Score
		})
		// Then truncate to TopK
		if len(searchResponse.Results) > int(req.TopK) {
			searchResponse.Results = searchResponse.Results[:req.TopK]
		}
		return searchResponse, nil
	}

	rerankedResults := make([]*pb.SearchResult, len(rerankResp.Results))
	for i, result := range rerankResp.Results {
		payload := make(map[string]string)
		for _, candidate := range candidates {
			if candidate.Id == result.Id {
				payload = candidate.Metadata
				break
			}
		}

		rerankedResults[i] = &pb.SearchResult{
			Id:      result.Id,
			Score:   result.RerankScore,
			Payload: payload,
		}
	}

	return &pb.SearchResponse{
		Results: rerankedResults,
	}, nil
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

// metricsMiddleware wraps the gRPC gateway mux to track request metrics
func metricsMiddleware(mgr *management.Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapped := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		// Record the request
		duration := time.Since(start)
		isError := wrapped.statusCode >= 400
		mgr.RecordRequest(r.URL.Path, r.Method, duration, isError)
	})
}

// responseRecorder wraps http.ResponseWriter to capture status code
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.statusCode = code
	rr.ResponseWriter.WriteHeader(code)
}

// managementHandlers returns a handler for management endpoints
func managementHandlers(mgr *management.Manager) http.Handler {
	mux := http.NewServeMux()

	// System health
	mux.HandleFunc("/v1/system/health", mgr.HandleSystemHealth)

	// Admin endpoints
	mux.HandleFunc("/v1/admin/stats", mgr.HandleGatewayStats)
	mux.HandleFunc("/v1/admin/workers", mgr.HandleWorkers)
	mux.HandleFunc("/v1/admin/collections", mgr.HandleCollections)

	// Alerts
	mux.HandleFunc("/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resolve") {
			mgr.HandleResolveAlert(w, r)
		} else {
			mgr.HandleAlerts(w, r)
		}
	})

	return mux
}

// Start initializes and runs the API Gateway's gRPC and HTTP servers.
func Start(config Config, grpcListener net.Listener) (*grpc.Server, *grpc.ClientConn) {
	// Establish gRPC connection to Auth service
	authConn, err := grpc.Dial(config.AuthServiceAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to Auth service: %v", err)
	}
	authClient := authpb.NewAuthServiceClient(authConn)

	// Establish gRPC connection to Reranker service
	rerankerConn, err := grpc.Dial(config.RerankerServiceAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to Reranker service: %v", err)
	}
	rerankerClient := reranker.NewRerankServiceClient(rerankerConn)

	// Initialize feedback service with SQLite
	feedbackService, err := feedback.NewService(config.FeedbackDBPath)
	if err != nil {
		log.Fatalf("Failed to initialize feedback service: %v", err)
	}

	// Create the gateway server with the list of PD addresses
	server := &gatewayServer{
		pdAddrs:         strings.Split(config.PlacementDriver, ","),
		authClient:      authClient,
		rerankerClient:  rerankerClient,
		feedbackService: feedbackService,
	}

	// Initialize the leader connection with retry logic
	maxRetries := 5
	baseBackoff := 1 * time.Second
	for i := 0; i < maxRetries; i++ {
		if _, err := server.updateLeader(); err == nil {
			break
		}
		if i == maxRetries-1 {
			log.Fatalf("Failed to initialize connection with placement driver leader after %d attempts", maxRetries)
		}
		wait := baseBackoff * time.Duration(1<<i)
		log.Printf("Failed to connect to placement driver leader, retrying in %v...", wait)
		time.Sleep(wait)
	}

	// Create management manager with gRPC client
	// Note: We need to pass a pb.VectronServiceClient, but we don't have a direct client.
	// For now, we'll pass nil and the management manager will work without it.
	managementMgr := management.NewManager(
		server.leader.client,
		authClient,
		feedbackService,
		nil, // grpcClient - not available directly
	)
	server.managementMgr = managementMgr

	// Create the gRPC server with a chain of unary interceptors for middleware.
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			middleware.AuthInterceptor(authClient, config.JWTSecret),
			middleware.LoggingInterceptor,
			middleware.RateLimitInterceptor(config.RateLimitRPS),
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

	// Create a combined HTTP handler that routes to either management or gRPC gateway
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a management endpoint
		if strings.HasPrefix(r.URL.Path, "/v1/system/") ||
			strings.HasPrefix(r.URL.Path, "/v1/admin/") ||
			strings.HasPrefix(r.URL.Path, "/v1/alerts") {
			managementHandlers(managementMgr).ServeHTTP(w, r)
		} else {
			mux.ServeHTTP(w, r)
		}
	})

	// Wrap with CORS middleware
	corsHandler := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, Content-Length, X-CSRF-Token, X-API-Key")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			h.ServeHTTP(w, r)
		})
	}

	// Wrap with metrics middleware
	finalHandler := metricsMiddleware(managementMgr, corsHandler(mainHandler))

	// Start the HTTP server in a separate goroutine.
	go func() {
		log.Printf("Vectron HTTP API (curl)      → %s", config.HTTPAddr)
		log.Printf("Management endpoints         → %s/v1/system/health, %s/v1/admin/...", config.HTTPAddr, config.HTTPAddr)
		log.Printf("Using placement driver           → %s", config.PlacementDriver)
		if err := http.ListenAndServe(config.HTTPAddr, finalHandler); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed to serve: %v", err)
		}
	}()

	return grpcServer, authConn
}

// ================ MAIN ================

func main() {
	_, authConn := Start(cfg, nil)
	defer authConn.Close()

	select {}
}
