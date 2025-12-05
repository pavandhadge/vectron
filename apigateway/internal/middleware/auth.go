// middleware/auth.go — FINAL VERSION
package middleware

import (
	"context"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Context keys — avoid collisions
type contextKey string

const (
	UserIDKey   contextKey = "user_id"
	PlanKey     contextKey = "plan"
	APIKeyIDKey contextKey = "api_key_id"
)

var jwtSecret []byte

func SetJWTSecret(secret string) {
	jwtSecret = []byte(secret)
}

// Your custom claims
type Claims struct {
	UserID   string `json:"user_id"`
	Plan     string `json:"plan"`       // "free", "pro", "enterprise"
	APIKeyID string `json:"api_key_id"` // which key was used
	jwt.RegisteredClaims
}

// AuthInterceptor — extracts and validates JWT, injects rich data
func AuthInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
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
		return jwtSecret, nil
	}, jwt.WithLeeway(5)) // 5 second clock skew tolerance

	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
	}
	if !token.Valid {
		return nil, status.Errorf(codes.Unauthenticated, "token expired or invalid")
	}
	//here add the apikey verification func

	// Inject everything into context
	ctx = context.WithValue(ctx, UserIDKey, claims.UserID)
	ctx = context.WithValue(ctx, PlanKey, claims.Plan)
	ctx = context.WithValue(ctx, APIKeyIDKey, claims.APIKeyID)

	return handler(ctx, req)
}

// Helper functions — use anywhere in your code
func GetUserID(ctx context.Context) string {
	if v := ctx.Value(UserIDKey); v != nil {
		return v.(string)
	}
	return ""
}

func GetPlan(ctx context.Context) string {
	if v := ctx.Value(PlanKey); v != nil {
		return v.(string)
	}
	return "free"
}

func GetAPIKeyID(ctx context.Context) string {
	if v := ctx.Value(APIKeyIDKey); v != nil {
		return v.(string)
	}
	return ""
}
