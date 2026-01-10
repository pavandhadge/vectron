package handler

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	etcdclient "github.com/pavandhadge/vectron/auth/service/internal/etcd"
	"github.com/pavandhadge/vectron/auth/service/internal/middleware"
	authpb "github.com/pavandhadge/vectron/auth/service/proto/auth"
)

// AuthServer implements the AuthServiceServer interface.
type AuthServer struct {
	authpb.UnimplementedAuthServiceServer
	store     *etcdclient.Client
	jwtSecret []byte
}

// NewAuthServer creates a new AuthServer.
func NewAuthServer(store *etcdclient.Client, jwtSecret string) *AuthServer {
	return &AuthServer{
		store:     store,
		jwtSecret: []byte(jwtSecret),
	}
}

// --- User Account Management ---

func (s *AuthServer) RegisterUser(ctx context.Context, req *authpb.RegisterUserRequest) (*authpb.RegisterUserResponse, error) {
	if req.Email == "" || req.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "Email and Password are required")
	}

	userData, err := s.store.CreateUser(ctx, req.Email, req.Password)
	if err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "failed to register user: %v", err)
	}

	return &authpb.RegisterUserResponse{
		User: &authpb.User{
			Id:        userData.ID,
			Email:     userData.Email,
			CreatedAt: userData.CreatedAt,
		},
	}, nil
}

func (s *AuthServer) Login(ctx context.Context, req *authpb.LoginRequest) (*authpb.LoginResponse, error) {
	userData, err := s.store.GetUserByEmail(ctx, req.Email)
	if err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(userData.HashedPassword), []byte(req.Password)); err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid password")
	}

	// Create JWT token
	claims := &middleware.Claims{
		UserID: userData.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate token: %v", err)
	}

	return &authpb.LoginResponse{
		JwtToken: tokenString,
		User: &authpb.User{
			Id:        userData.ID,
			Email:     userData.Email,
			CreatedAt: userData.CreatedAt,
		},
	}, nil
}

func (s *AuthServer) GetUserProfile(ctx context.Context, req *authpb.GetUserProfileRequest) (*authpb.GetUserProfileResponse, error) {
	userID, err := middleware.GetUserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	userData, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "user profile not found: %v", err)
	}

	return &authpb.GetUserProfileResponse{
		User: &authpb.User{
			Id:        userData.ID,
			Email:     userData.Email,
			CreatedAt: userData.CreatedAt,
		},
	}, nil
}

// --- API Key Management (Authenticated) ---

func (s *AuthServer) CreateAPIKey(ctx context.Context, req *authpb.CreateAPIKeyRequest) (*authpb.CreateAPIKeyResponse, error) {
	userID, err := middleware.GetUserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "Name is required")
	}

	fullKey, keyData, err := s.store.CreateAPIKey(ctx, userID, req.Name)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create key: %v", err)
	}

	return &authpb.CreateAPIKeyResponse{
		FullKey: fullKey,
		KeyInfo: &authpb.APIKey{
			KeyPrefix: keyData.KeyPrefix,
			UserId:    keyData.UserID,
			CreatedAt: keyData.CreatedAt,
			Name:      keyData.Name,
		},
	}, nil
}

func (s *AuthServer) ListAPIKeys(ctx context.Context, req *authpb.ListAPIKeysRequest) (*authpb.ListAPIKeysResponse, error) {
	userID, err := middleware.GetUserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	keys, err := s.store.ListAPIKeys(ctx, userID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list keys: %v", err)
	}

	return &authpb.ListAPIKeysResponse{Keys: keys}, nil
}

func (s *AuthServer) DeleteAPIKey(ctx context.Context, req *authpb.DeleteAPIKeyRequest) (*authpb.DeleteAPIKeyResponse, error) {
	userID, err := middleware.GetUserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if req.KeyPrefix == "" {
		return nil, status.Error(codes.InvalidArgument, "KeyPrefix is required")
	}

	success, err := s.store.DeleteAPIKey(ctx, req.KeyPrefix, userID)
	if err != nil {
		return nil, status.Errorf(codes.PermissionDenied, "failed to delete key: %v", err)
	}

	return &authpb.DeleteAPIKeyResponse{Success: success}, nil
}

// --- Internal Service RPCs ---

func (s *AuthServer) ValidateAPIKey(ctx context.Context, req *authpb.ValidateAPIKeyRequest) (*authpb.ValidateAPIKeyResponse, error) {
	if req.FullKey == "" {
		return nil, status.Error(codes.InvalidArgument, "FullKey is required")
	}

	keyData, valid, err := s.store.ValidateAPIKey(ctx, req.FullKey)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to validate key: %v", err)
	}
	if !valid {
		return &authpb.ValidateAPIKeyResponse{Valid: false}, nil
	}

	return &authpb.ValidateAPIKeyResponse{
		Valid:  true,
		UserId: keyData.UserID,
		Plan:   keyData.Plan,
	}, nil
}
