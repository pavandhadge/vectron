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
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
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
	// Validate email format
	if err := middleware.ValidateEmail(req.Email); err != nil {
		return nil, err
	}

	// Validate password strength
	if err := middleware.ValidatePasswordStrict(req.Password); err != nil {
		return nil, err
	}

	// Normalize email
	email := middleware.NormalizeEmail(req.Email)

	// Create user
	userData, err := s.store.CreateUser(ctx, email, req.Password)
	if err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "failed to register user: %v", err)
	}

	return &authpb.RegisterUserResponse{
		User: &authpb.UserProfile{
			Id:                 userData.ID,
			Email:              userData.Email,
			CreatedAt:          userData.CreatedAt,
			Plan:               userData.Plan,
			SubscriptionStatus: userData.SubscriptionStatus,
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

	// Create JWT token (Login JWT) - does NOT contain APIKey claim, but contains Plan
	claims := &middleware.Claims{
		UserID: userData.ID,
		APIKey: "",                     // APIKey claim is empty for Login JWT
		Plan:   userData.Plan.String(), // Include Plan in Login JWT
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
		User: &authpb.UserProfile{
			Id:                 userData.ID,
			Email:              userData.Email,
			CreatedAt:          userData.CreatedAt,
			Plan:               userData.Plan,
			SubscriptionStatus: userData.SubscriptionStatus,
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
		User: &authpb.UserProfile{
			Id:                 userData.ID,
			Email:              userData.Email,
			CreatedAt:          userData.CreatedAt,
			Plan:               userData.Plan,
			SubscriptionStatus: userData.SubscriptionStatus,
		},
	}, nil
}

func (s *AuthServer) UpdateUserProfile(ctx context.Context, req *authpb.UpdateUserProfileRequest) (*authpb.UpdateUserProfileResponse, error) {
	userID, err := middleware.GetUserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	userData, err := s.store.UpdateUserPlan(ctx, userID, req.Plan)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update user profile: %v", err)
	}

	// Issue a new token with the updated plan claim
	newClaims := &middleware.Claims{
		UserID: userData.ID,
		APIKey: "", // APIKey claim is empty for Login JWT
		Plan:   userData.Plan.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, newClaims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate token after plan update: %v", err)
	}

	return &authpb.UpdateUserProfileResponse{
		User: &authpb.UserProfile{
			Id:                 userData.ID,
			Email:              userData.Email,
			CreatedAt:          userData.CreatedAt,
			Plan:               userData.Plan,
			SubscriptionStatus: userData.SubscriptionStatus,
		},
		JwtToken: tokenString,
	}, nil
}

// --- API Key Management (Authenticated) ---

func (s *AuthServer) CreateAPIKey(ctx context.Context, req *authpb.CreateAPIKeyRequest) (*authpb.CreateAPIKeyResponse, error) {
	userID, err := middleware.GetUserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Validate API key name
	if err := middleware.ValidateAPIKeyName(req.Name); err != nil {
		return nil, err
	}

	// Sanitize the name
	name := middleware.SanitizeString(req.Name)

	fullKey, keyData, err := s.store.CreateAPIKey(ctx, userID, name)
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

// Create an SDK JWT for an existing API key.
func (s *AuthServer) CreateSDKJWT(ctx context.Context, req *authpb.CreateSDKJWTRequest) (*authpb.CreateSDKJWTResponse, error) {
	userID, err := middleware.GetUserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if req.ApiKeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "API Key ID is required")
	}

	// Validate that the API Key ID belongs to the user
	// We call ValidateAPIKey with the API Key ID as the full key.
	// This is a temporary workaround. A proper solution would be to have
	// a specific internal RPC to verify API key ownership.
	// For now, assume a dummy full key that matches the KeyPrefix for validation purposes.
	// In a real system, you'd retrieve the actual fullKey if it were securely stored,
	// or perform validation differently. Here, we'll try to retrieve the APIKeyData directly
	// to get the actual APIKeyId for validation.
	keyData, err := s.store.GetAPIKeyDataByPrefix(ctx, req.ApiKeyId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get API key data: %v", err)
	}
	if keyData == nil || keyData.UserID != userID {
		return nil, status.Errorf(codes.PermissionDenied, "invalid API key ID or not owned by user")
	}

	// Retrieve user data to get the plan
	userData, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get user data: %v", err)
	}

	// Create SDK JWT
	claims := &middleware.Claims{
		UserID: userData.ID,
		APIKey: req.ApiKeyId, // The APIKey claim now holds the KeyPrefix
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)), // Longer expiration for SDK JWTs
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate SDK JWT: %v", err)
	}

	return &authpb.CreateSDKJWTResponse{
		SdkJwt: tokenString,
	}, nil
}

// GetAuthDetailsForSDK is an internal RPC for the API Gateway to get user details from an API Key ID.
func (s *AuthServer) GetAuthDetailsForSDK(ctx context.Context, req *authpb.GetAuthDetailsForSDKRequest) (*authpb.GetAuthDetailsForSDKResponse, error) {
	if req.ApiKeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "API Key ID is required")
	}

	keyData, err := s.store.GetAPIKeyDataByPrefix(ctx, req.ApiKeyId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get API key data: %v", err)
	}
	if keyData == nil {
		return &authpb.GetAuthDetailsForSDKResponse{Success: false}, nil
	}

	userData, err := s.store.GetUserByID(ctx, keyData.UserID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "user not found for API key: %v", err)
	}

	return &authpb.GetAuthDetailsForSDKResponse{
		Success: true,
		UserId:  userData.ID,
		Plan:    userData.Plan,
	}, nil
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
		Valid:    true,
		UserId:   keyData.UserID,
		Plan:     authpb.Plan_name[int32(keyData.Plan)],
		ApiKeyId: keyData.KeyPrefix,
	}, nil
}
