// middleware/logging.go
package middleware

import (
	"context"
	"log"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

type contextKey string

const UserIDKey contextKey = "user_id"

// === gRPC Unary Interceptor (for gRPC requests) ===
func LoggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	start := time.Now()
	userID := extractUserID(ctx)

	// Extract client IP if available
	clientIP := "unknown"
	if p, ok := peer.FromContext(ctx); ok {
		clientIP = p.Addr.String()
	}

	log.Printf("[gRPC] START] method=%s user=%s client=%s", info.FullMethod, userID, clientIP)

	resp, err := handler(ctx, req)

	duration := time.Since(start)
	status := "OK"
	if err != nil {
		status = "ERROR"
	}

	log.Printf("[gRPC] %s] method=%s user=%s duration=%v", status, info.FullMethod, userID, duration)
	return resp, err
}

// === HTTP Middleware (for HTTP/JSON gateway) ===
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		userID, _ := r.Context().Value(UserIDKey).(string)
		if userID == "" {
			userID = "anonymous"
		}

		log.Printf("[HTTP START] %s %s user=%s client=%s", r.Method, r.URL.Path, userID, r.RemoteAddr)

		next.ServeHTTP(w, r)

		log.Printf("[HTTP DONE] %s %s user=%s duration=%v", r.Method, r.URL.Path, userID, time.Since(start))
	})
}

// Helper: extract user ID from context (set by auth)
func extractUserID(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get("user_id"); len(values) > 0 {
			return values[0]
		}
	}
	if userID := ctx.Value(UserIDKey); userID != nil {
		return userID.(string)
	}
	return "anonymous"
}
