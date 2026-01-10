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
)

type contextKey string

const UserIDKey contextKey = "user_id"

// Claims defines the custom claims for JWT.
type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

type AuthInterceptor struct {
	jwtSecret       []byte
	unprotectedRPCs map[string]bool
}

func NewAuthInterceptor(secret string, unprotectedRPCs []string) *AuthInterceptor {
	unprotectedMap := make(map[string]bool)
	for _, rpc := range unprotectedRPCs {
		unprotectedMap[rpc] = true
	}
	return &AuthInterceptor{
		jwtSecret:       []byte(secret),
		unprotectedRPCs: unprotectedMap,
	}
}

// Unary returns a gRPC unary interceptor for JWT authentication.
func (ai *AuthInterceptor) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Check if the RPC is unprotected.
		if ai.unprotectedRPCs[info.FullMethod] {
			return handler(ctx, req)
		}

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
			return ai.jwtSecret, nil
		}, jwt.WithLeeway(5*time.Second))

		if err != nil || !token.Valid {
			return nil, status.Errorf(codes.Unauthenticated, "invalid or expired token")
		}

		// Inject user ID into the context.
		ctx = context.WithValue(ctx, UserIDKey, claims.UserID)

		return handler(ctx, req)
	}
}

// GetUserIDFromContext safely retrieves the user ID from the context.
func GetUserIDFromContext(ctx context.Context) (string, error) {
	userID, ok := ctx.Value(UserIDKey).(string)
	if !ok || userID == "" {
		return "", status.Error(codes.Unauthenticated, "missing user ID in context")
	}
	return userID, nil
}
