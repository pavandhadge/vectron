// This file implements logging middleware for the API Gateway.
// It provides a gRPC unary interceptor and an HTTP middleware to log details
// about incoming requests, including the method, user, client IP, and duration.

package middleware

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// LoggingInterceptor is a gRPC unary interceptor that logs information about each request.
// It logs the start and end of a request, including the gRPC method, user ID, client IP, and total duration.
func LoggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	start := time.Now()

	// Attempt to get user and client details from the context.
	userID := GetUserID(ctx) // Use the helper from auth.go
	if userID == "" {
		userID = "anonymous"
	}

	clientIP := "unknown"
	if p, ok := peer.FromContext(ctx); ok {
		clientIP = p.Addr.String()
	}

	logThis := shouldSampleGatewayLog()
	if logThis {
		log.Printf("[gRPC][START] method=%s user=%s client=%s", info.FullMethod, userID, clientIP)
	}

	// Call the next handler in the chain.
	resp, err := handler(ctx, req)

	duration := time.Since(start)
	status := "OK"
	if err != nil {
		status = "ERROR"
	}

	if logThis {
		log.Printf("[gRPC][END] status=%s method=%s user=%s duration=%v", status, info.FullMethod, userID, duration)
	}
	return resp, err
}

// Logging is an HTTP middleware that provides request logging for the REST/JSON gateway.
// It's not used in the current gRPC-only setup but is available for future use.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		userID := GetUserID(r.Context())
		if userID == "" {
			userID = "anonymous"
		}

		logThis := shouldSampleGatewayLog()
		if logThis {
			log.Printf("[HTTP][START] method=%s path=%s user=%s client=%s", r.Method, r.URL.Path, userID, r.RemoteAddr)
		}

		// Serve the next handler.
		next.ServeHTTP(w, r)

		if logThis {
			log.Printf("[HTTP][END] method=%s path=%s user=%s duration=%v", r.Method, r.URL.Path, userID, time.Since(start))
		}
	})
}

var (
	gatewayLogSampleN = envInt("VECTRON_GATEWAY_LOG_SAMPLE_N", 1)
	gatewayLogCounter uint64
)

func shouldSampleGatewayLog() bool {
	n := atomic.LoadInt64(&gatewayLogSampleN)
	if n <= 1 {
		return true
	}
	return atomic.AddUint64(&gatewayLogCounter, 1)%uint64(n) == 0
}

func envInt(key string, def int64) int64 {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	parsed, err := strconv.ParseInt(val, 10, 64)
	if err != nil || parsed <= 0 {
		return def
	}
	return parsed
}

// extractUserID is a helper function to safely retrieve a user ID from the context.
// It checks both gRPC metadata and the context value set by the auth interceptor.
// Note: This is somewhat redundant as GetUserID from auth.go is preferred.
func extractUserID(ctx context.Context) string {
	// First, try the value injected by the AuthInterceptor.
	if userID := GetUserID(ctx); userID != "" {
		return userID
	}
	// Fallback for cases where metadata might be used directly.
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get("user_id"); len(values) > 0 {
			return values[0]
		}
	}
	return "anonymous"
}
