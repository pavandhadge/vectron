// This file implements rate limiting middleware for the API Gateway.
// It provides a gRPC unary interceptor that enforces a requests-per-second (RPS)
// limit on a per-user basis.

package middleware

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// limiter tracks the request count for a user within a specific time window.
type limiter struct {
	count   int       // Number of requests made in the current window.
	resetAt time.Time // The time when the count should be reset.
}

var (
	// mu protects concurrent access to the limits map.
	mu sync.Mutex
	// limits stores the rate limiter state for each user ID.
	limits = make(map[string]*limiter)
)

// RateLimitInterceptor returns a gRPC unary interceptor that enforces a per-user rate limit.
// It uses a simple in-memory map to track request counts for each user.
func RateLimitInterceptor(rps int) func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		fmt.Println("ratelimiter started")
		// The user ID should be injected into the context by the AuthInterceptor.
		userID := GetUserID(ctx)
		if userID == "" {
			// Fail open or closed? For now, we require a user ID to apply a limit.
			// Could also use IP address as a fallback.
			return handler(ctx, req)
		}

		mu.Lock()
		defer mu.Unlock()

		now := time.Now()
		l, exists := limits[userID]

		// If the user is not in the map or their window has expired, reset their limit.
		if !exists || now.After(l.resetAt) {
			l = &limiter{
				count:   1,
				resetAt: now.Add(time.Second), // The window is one second.
			}
			limits[userID] = l
		} else {
			// Otherwise, just increment the count.
			l.count++
		}

		// Check if the user has exceeded the allowed requests per second.
		if l.count > rps {
			return nil, status.Errorf(codes.ResourceExhausted, "rate limit of %d RPS exceeded", rps)
		}
		fmt.Println("ratelimiter ended")

		return handler(ctx, req)
	}
}
