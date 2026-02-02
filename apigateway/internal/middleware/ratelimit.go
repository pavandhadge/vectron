// This file implements rate limiting middleware for the API Gateway.
// It provides a gRPC unary interceptor that enforces a requests-per-second (RPS)
// limit on a per-user basis.
// Optimized with sharded locks to reduce contention under high concurrency.

package middleware

import (
	"context"
	"fmt"
	"hash/fnv"
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

const shardCount = 256 // Number of shards for lock distribution

// shardedRateLimiter uses multiple locks to reduce contention
type shardedRateLimiter struct {
	mu     [shardCount]sync.Mutex
	limits [shardCount]map[string]*limiter
}

var rateLimiter = &shardedRateLimiter{}

func init() {
	// Initialize the limiter maps for each shard
	for i := 0; i < shardCount; i++ {
		rateLimiter.limits[i] = make(map[string]*limiter)
	}
	// Start cleanup goroutine for expired entries
	go cleanupExpiredLimiters()
}

// getShard returns the shard index for a user ID
func getShard(userID string) int {
	h := fnv.New32a()
	h.Write([]byte(userID))
	return int(h.Sum32()) % shardCount
}

// cleanupExpiredLimiters periodically removes expired rate limit entries
func cleanupExpiredLimiters() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		for i := 0; i < shardCount; i++ {
			rateLimiter.mu[i].Lock()
			for userID, l := range rateLimiter.limits[i] {
				if now.After(l.resetAt) {
					delete(rateLimiter.limits[i], userID)
				}
			}
			rateLimiter.mu[i].Unlock()
		}
	}
}

// RateLimitInterceptor returns a gRPC unary interceptor that enforces a per-user rate limit.
// It uses a sharded in-memory map to track request counts for each user, reducing lock contention.
func RateLimitInterceptor(rps int) func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// The user ID should be injected into the context by the AuthInterceptor.
		userID := GetUserID(ctx)
		if userID == "" {
			// Fail open or closed? For now, we require a user ID to apply a limit.
			// Could also use IP address as a fallback.
			return handler(ctx, req)
		}

		// Get the appropriate shard for this user
		shard := getShard(userID)
		rateLimiter.mu[shard].Lock()

		now := time.Now()
		l, exists := rateLimiter.limits[shard][userID]

		// If the user is not in the map or their window has expired, reset their limit.
		if !exists || now.After(l.resetAt) {
			l = &limiter{
				count:   1,
				resetAt: now.Add(time.Second), // The window is one second.
			}
			rateLimiter.limits[shard][userID] = l
		} else {
			// Otherwise, just increment the count.
			l.count++
		}

		// Check if the user has exceeded the allowed requests per second.
		exceeded := l.count > rps
		rateLimiter.mu[shard].Unlock()

		if exceeded {
			return nil, status.Errorf(codes.ResourceExhausted, "rate limit of %d RPS exceeded", rps)
		}
		fmt.Println("ratelimiter ended")

		return handler(ctx, req)
	}
}
