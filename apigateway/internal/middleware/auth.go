// This file implements the authentication middleware for the API Gateway.
// It provides a gRPC unary interceptor that validates JWT tokens from incoming requests,
// extracts custom claims, and injects user and plan information into the request context.

package middleware

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
)

// contextKey is a custom type to avoid collisions with other context keys.
type contextKey string

// Defines the keys used to store user and plan information in the context.
const (
	UserIDKey   contextKey = "user_id"
	PlanKey     contextKey = "plan"
	APIKeyIDKey contextKey = "api_key_id"
)

// AuthInterceptor returns a gRPC unary interceptor that performs API key based authentication
// by delegating validation to the Auth service.
func AuthInterceptor(authClient authpb.AuthServiceClient) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Errorf(codes.Unauthenticated, "missing metadata")
		}

		auths := md.Get("authorization")
		if len(auths) == 0 {
			return nil, status.Errorf(codes.Unauthenticated, "missing Authorization header")
		}

		// Expecting "Bearer <token>" or raw API key
		fullAPIKey := strings.TrimSpace(strings.TrimPrefix(auths[0], "Bearer"))
		if fullAPIKey == "" {
			fullAPIKey = strings.TrimSpace(auths[0]) // Try raw API key if Bearer prefix not found
		}

		if fullAPIKey == "" {
			return nil, status.Errorf(codes.Unauthenticated, "invalid Authorization format or empty API key")
		}

		// Call Auth service to validate the API key
		validateResp, err := authClient.ValidateAPIKey(ctx, &authpb.ValidateAPIKeyRequest{FullKey: fullAPIKey})
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "API key validation failed: %v", err)
		}

		if !validateResp.Valid {
			return nil, status.Errorf(codes.Unauthenticated, "invalid API key")
		}

		// Inject user and plan info into the context for use in other handlers.
		ctx = context.WithValue(ctx, UserIDKey, validateResp.UserId)
		ctx = context.WithValue(ctx, PlanKey, validateResp.Plan)
		ctx = context.WithValue(ctx, APIKeyIDKey, validateResp.ApiKeyId)

		// Pass control to the next handler.
		return handler(ctx, req)
	}
}

// GetUserID is a helper function to safely retrieve the user ID from the context.
func GetUserID(ctx context.Context) string {
	if v, ok := ctx.Value(UserIDKey).(string); ok {
		return v
	}
	return ""
}

// GetPlan is a helper function to safely retrieve the user's plan from the context.
// It defaults to "free" if the plan is not set.
func GetPlan(ctx context.Context) string {
	if v, ok := ctx.Value(PlanKey).(string); ok {
		return v
	}
	return "free"
}

// GetAPIKeyID is a helper function to safely retrieve the API key ID from the context.
func GetAPIKeyID(ctx context.Context) string {
	if v, ok := ctx.Value(APIKeyIDKey).(string); ok {
		return v
	}
	return ""
}
