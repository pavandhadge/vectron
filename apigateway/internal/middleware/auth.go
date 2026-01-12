// This file implements the authentication middleware for the API Gateway.
// It provides a gRPC unary interceptor that validates JWT tokens from incoming requests,
// extracts custom claims, and injects user and plan information into the request context.

package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

// Claims defines the custom claims for JWT.
type Claims struct {
	UserID string `json:"user_id"`
	APIKey string `json:"api_key"`
	Plan   string `json:"plan"`
	jwt.RegisteredClaims
}

// AuthInterceptor returns a gRPC unary interceptor that performs JWT-based authentication.
func AuthInterceptor(authClient authpb.AuthServiceClient, jwtSecret string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Errorf(codes.Unauthenticated, "missing metadata")
		}

		auths := md.Get("authorization")
		if len(auths) == 0 {
			return nil, status.Errorf(codes.Unauthenticated, "missing Authorization header")
		}

		tokenStr := strings.TrimSpace(strings.TrimPrefix(auths[0], "Bearer"))
		if tokenStr == "" {
			return nil, status.Errorf(codes.Unauthenticated, "invalid Authorization format")
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		}, jwt.WithLeeway(5*time.Second))

		if err != nil || !token.Valid {
			return nil, status.Errorf(codes.Unauthenticated, "invalid or expired token")
		}

		// Distinguish between Login JWT and SDK JWT
		if claims.APIKey != "" {
			// This is an SDK JWT. The APIKey field in the claim holds the API Key ID (prefix).
			// We call the internal GetAuthDetailsForSDK RPC to get the user ID and plan.
			authDetails, err := authClient.GetAuthDetailsForSDK(ctx, &authpb.GetAuthDetailsForSDKRequest{ApiKeyId: claims.APIKey})
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get auth details for SDK token: %v", err)
			}
			if !authDetails.Success {
				return nil, status.Errorf(codes.Unauthenticated, "invalid API key ID in SDK token")
			}

			// Inject user and plan info into the context for use in other handlers.
			ctx = context.WithValue(ctx, UserIDKey, authDetails.UserId)
			ctx = context.WithValue(ctx, PlanKey, authDetails.Plan.String()) // Store plan as string
			ctx = context.WithValue(ctx, APIKeyIDKey, claims.APIKey)
		} else {
			// This is a Login JWT. Inject user ID and Plan from the JWT claims.
			ctx = context.WithValue(ctx, UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, PlanKey, claims.Plan)
			ctx = context.WithValue(ctx, APIKeyIDKey, "") // No API Key ID for Login JWT
		}

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
// It defaults to Plan_FREE if the plan is not set.
func GetPlan(ctx context.Context) authpb.Plan {
	if v, ok := ctx.Value(PlanKey).(string); ok {
		// Convert string back to authpb.Plan
		if p, ok := authpb.Plan_value[v]; ok {
			return authpb.Plan(p)
		}
	}
	return authpb.Plan_FREE
}

// GetAPIKeyID is a helper function to safely retrieve the API key ID from the context.
func GetAPIKeyID(ctx context.Context) string {
	if v, ok := ctx.Value(APIKeyIDKey).(string); ok {
		return v
	}
	return ""
}
