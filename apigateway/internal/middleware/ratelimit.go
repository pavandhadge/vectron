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

type limiter struct {
	count   int
	resetAt time.Time
}

var (
	mu     sync.Mutex
	limits = make(map[string]*limiter)
)

func RateLimitInterceptor(rps int) func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		fmt.Println("ratelimited started")
		userID, ok := ctx.Value(UserIDKey).(string)
		if !ok {
			return nil, status.Errorf(codes.Unauthenticated, "no user")
		}

		mu.Lock()
		l := limits[userID]
		now := time.Now()
		if l == nil || now.After(l.resetAt) {
			l = &limiter{count: 1, resetAt: now.Add(time.Second)}
			limits[userID] = l
		} else {
			l.count++
		}
		remaining := rps - l.count
		mu.Unlock()

		if remaining < 0 {
			return nil, status.Errorf(codes.ResourceExhausted, "rate limit exceeded")
		}
		fmt.Println("logger ended")

		return handler(ctx, req)
	}
}
