// This file implements the authentication middleware for the API Gateway.
// It provides a gRPC unary interceptor that validates JWT tokens from incoming requests,
// extracts custom claims, and injects user and plan information into the request context.

package middleware

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// contextKey is a custom type to avoid collisions with other context keys.
type contextKey string

// Defines the keys used to store user and plan information in the context.
const (
	UserIDKey   contextKey = "user_id"
	PlanKey     contextKey = "plan"
	APIKeyIDKey contextKey = "api_key_id"
)

// jwtSecret stores the secret key used to validate JWT signatures.
var jwtSecret []byte

// SetJWTSecret configures the JWT secret for the authentication middleware.
// This must be called at startup.
func SetJWTSecret(secret string) {
	jwtSecret = []byte(secret)
}

// Claims defines the custom claims expected in the JWT payload.
// It includes standard JWT claims and application-specific data like user ID and plan.
type Claims struct {
	UserID   string `json:"user_id"`
	Plan     string `json:"plan"`       // e.g., "free", "pro", "enterprise"
	APIKeyID string `json:"api_key_id"` // The ID of the API key used for the request.
	jwt.RegisteredClaims
}

// AuthInterceptor is a gRPC unary interceptor that performs JWT-based authentication.
// It extracts the token from the 'authorization' header, validates it,
// and injects the claims' data into the context for downstream handlers to use.
func AuthInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	fmt.Print("auth activated")
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Errorf(codes.Unauthenticated, "missing metadata")
	}

	auths := md.Get("authorization")
	if len(auths) == 0 {
		return nil, status.Errorf(codes.Unauthenticated, "missing Authorization header")
	}

	// Expecting "Bearer <token>"
	tokenStr := strings.TrimSpace(strings.TrimPrefix(auths[0], "Bearer"))
	if tokenStr == "" {
		return nil, status.Errorf(codes.Unauthenticated, "invalid Authorization format")
	}

	// Parse and validate the token.
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		// Check the signing method
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return jwtSecret, nil
	}, jwt.WithLeeway(5*time.Second)) // Allow for minor clock skew.

	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
	}
	if !token.Valid {
		return nil, status.Errorf(codes.Unauthenticated, "token expired or invalid")
	}
	// TODO: Implement additional verification, e.g., checking API key status in a database.

	// Inject user and plan info into the context for use in other handlers.
	ctx = context.WithValue(ctx, UserIDKey, claims.UserID)
	ctx = context.WithValue(ctx, PlanKey, claims.Plan)
	ctx = context.WithValue(ctx, APIKeyIDKey, claims.APIKeyID)

	// Pass control to the next handler.
	return handler(ctx, req)
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
