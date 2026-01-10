package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"testing"

	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	apigatewaypb "github.com/pavandhadge/vectron/shared/proto/apigateway"
	placementdriverpb "github.com/pavandhadge/vectron/shared/proto/placementdriver"
)

// MockAuthService is a mock implementation of the Auth service for testing.
type MockAuthService struct {
	authpb.UnimplementedAuthServiceServer
	validateAPIKeyFunc func(ctx context.Context, req *authpb.ValidateAPIKeyRequest) (*authpb.ValidateAPIKeyResponse, error)
}

func (m *MockAuthService) ValidateAPIKey(ctx context.Context, req *authpb.ValidateAPIKeyRequest) (*authpb.ValidateAPIKeyResponse, error) {
	if m.validateAPIKeyFunc != nil {
		return m.validateAPIKeyFunc(ctx, req)
	}
	return &authpb.ValidateAPIKeyResponse{Valid: false}, nil
}

// startMockAuthService starts a gRPC server with the MockAuthService.
func startMockAuthService(mockAuth *MockAuthService) (string, func()) {
	lis, err := net.Listen("tcp", "localhost:0") // Random available port
	if err != nil {
		log.Fatalf("Failed to listen for mock Auth service: %v", err)
	}
	s := grpc.NewServer()
	authpb.RegisterAuthServiceServer(s, mockAuth)
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Printf("Mock Auth service stopped: %v", err)
		}
	}()
	return lis.Addr().String(), s.Stop
}

// MockPlacementDriverService is a mock implementation of the Placement Driver service.
type MockPlacementDriverService struct {
	// For now, we might not need extensive mocking for PD for auth tests.
	// We can add methods as needed.
	placementdriverpb.UnimplementedPlacementServiceServer
}

func (m *MockPlacementDriverService) ListCollections(ctx context.Context, req *placementdriverpb.ListCollectionsRequest) (*placementdriverpb.ListCollectionsResponse, error) {
	return &placementdriverpb.ListCollectionsResponse{}, nil
}

func (m *MockPlacementDriverService) CreateCollection(ctx context.Context, req *placementdriverpb.CreateCollectionRequest) (*placementdriverpb.CreateCollectionResponse, error) {
	return &placementdriverpb.CreateCollectionResponse{Success: true}, nil
}

// startMockPlacementDriverService starts a gRPC server with the MockPlacementDriverService.
func startMockPlacementDriverService(mockPD *MockPlacementDriverService) (string, func()) {
	lis, err := net.Listen("tcp", "localhost:0") // Random available port
	if err != nil {
		log.Fatalf("Failed to listen for mock Placement Driver service: %v", err)
	}
	s := grpc.NewServer()
	placementdriverpb.RegisterPlacementServiceServer(s, mockPD)
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Printf("Mock Placement Driver service stopped: %v", err)
		}
	}()
	return lis.Addr().String(), s.Stop
}

func TestAPIGatewayAuthentication(t *testing.T) {
	// 1. Setup Mock Auth Service
	mockAuth := &MockAuthService{}
	authServiceAddr, stopAuthService := startMockAuthService(mockAuth)
	defer stopAuthService()

	// 2. Setup Mock Placement Driver Service
	mockPD := &MockPlacementDriverService{}
	pdServiceAddr, stopPDService := startMockPlacementDriverService(mockPD)
	defer stopPDService()

	// 3. Configure and Start API Gateway
	// Create config for testing
	testConfig := Config{
		GRPCAddr:        "localhost:0", // Random port for APIGW gRPC
		HTTPAddr:        "localhost:0", // Random port for APIGW HTTP
		PlacementDriver: pdServiceAddr,
		AuthServiceAddr: authServiceAddr,
		RateLimitRPS:    100,
	}

	apigwGRPCListener, err := net.Listen("tcp", testConfig.GRPCAddr)
	if err != nil {
		t.Fatalf("Failed to listen for APIGW gRPC: %v", err)
	}
	defer apigwGRPCListener.Close() // Ensure the listener is closed after the test

	testConfig.GRPCAddr = apigwGRPCListener.Addr().String() // Update config with actual port
	apigwGRPCAddr := testConfig.GRPCAddr

	var grpcServer *grpc.Server
	var authConn *grpc.ClientConn
	serverReady := make(chan struct{})

	go func() {
		defer close(serverReady) // Signal that the goroutine has completed its initial setup
		// Start the API Gateway with the test configuration and the pre-configured listener
		grpcServer, authConn = Start(testConfig, apigwGRPCListener)
	}()

	<-serverReady // Wait for the server to be ready before proceeding

	// Ensure the gRPC server and auth connection are gracefully closed after all tests are run
	defer func() {
		if grpcServer != nil {
			grpcServer.GracefulStop()
		}
		if authConn != nil {
			authConn.Close()
		}
	}()

	// 4. Create an API Gateway client for testing
	conn, err := grpc.Dial(apigwGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial APIGateway: %v", err)
	}
	defer conn.Close()
	apigwClient := apigatewaypb.NewVectronServiceClient(conn)

	tests := []struct {
		name       string
		apiKey     string
		mockAuthFn func(ctx context.Context, req *authpb.ValidateAPIKeyRequest) (*authpb.ValidateAPIKeyResponse, error)
		expectCode codes.Code
	}{
		{
			name:   "Valid API Key",
			apiKey: "valid-key",
			mockAuthFn: func(ctx context.Context, req *authpb.ValidateAPIKeyRequest) (*authpb.ValidateAPIKeyResponse, error) {
				if req.FullKey == "valid-key" {
					return &authpb.ValidateAPIKeyResponse{
						Valid:      true,
						UserId:     "test-user-id",
						Plan:       "pro",
						ApiKeyId: "valid-key-id",
					}, nil
				}
				return &authpb.ValidateAPIKeyResponse{Valid: false}, nil
			},
			expectCode: codes.OK,
		},
		{
			name:   "Invalid API Key",
			apiKey: "invalid-key",
			mockAuthFn: func(ctx context.Context, req *authpb.ValidateAPIKeyRequest) (*authpb.ValidateAPIKeyResponse, error) {
				return &authpb.ValidateAPIKeyResponse{Valid: false}, nil
			},
			expectCode: codes.Unauthenticated,
		},
		{
			name:   "Auth Service Error",
			apiKey: "error-key",
			mockAuthFn: func(ctx context.Context, req *authpb.ValidateAPIKeyRequest) (*authpb.ValidateAPIKeyResponse, error) {
				return nil, status.Error(codes.Internal, "mock auth service error")
			},
			expectCode: codes.Unauthenticated,
		},
		{
			name:       "Missing API Key",
			apiKey:     "",
			mockAuthFn: nil, // Should not even call mockAuthFn if key is missing
			expectCode: codes.Unauthenticated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAuth.validateAPIKeyFunc = tt.mockAuthFn

			ctx := context.Background()
			if tt.apiKey != "" {
				ctx = metadata.AppendToOutgoingContext(ctx, "authorization", fmt.Sprintf("Bearer %s", tt.apiKey))
			}

			// Use a simple RPC like CreateCollection for testing authentication
			_, err := apigwClient.CreateCollection(ctx, &apigatewaypb.CreateCollectionRequest{
				Name:      "test_collection",
				Dimension: 128,
				Distance:  "Cosine",
			})

			if tt.expectCode == codes.OK {
				if err != nil {
					t.Errorf("Expected success, got error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Expected error with code %s, got success", tt.expectCode)
				}
				s, ok := status.FromError(err)
				if !ok {
					t.Errorf("Expected gRPC error, got non-gRPC error: %v", err)
				}
				if s.Code() != tt.expectCode {
					t.Errorf("Expected error code %s, got %s: %v", tt.expectCode, s.Code(), err)
				}
			}
		})
	}
}