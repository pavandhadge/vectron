package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// RateLimiter provides rate limiting for gRPC endpoints
type RateLimiter struct {
	// requests tracks client -> endpoint -> request timestamps
	requests map[string]map[string][]time.Time
	mu       sync.RWMutex
	// limits defines rate limits per endpoint (requests per window)
	limits map[string]RateLimit
	// defaultLimit applies to endpoints not explicitly configured
	defaultLimit RateLimit
}

// RateLimit defines the limit configuration
type RateLimit struct {
	Requests int           // Number of requests allowed
	Window   time.Duration // Time window for the limit
}

// NewRateLimiter creates a new rate limiter with default limits
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		requests: make(map[string]map[string][]time.Time),
		limits:   make(map[string]RateLimit),
		defaultLimit: RateLimit{
			Requests: 100,
			Window:   time.Minute,
		},
	}
}

// SetLimit configures rate limit for a specific endpoint
func (rl *RateLimiter) SetLimit(endpoint string, requests int, window time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.limits[endpoint] = RateLimit{
		Requests: requests,
		Window:   window,
	}
}

// Unary returns a gRPC unary interceptor for rate limiting
func (rl *RateLimiter) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		clientID := rl.extractClientID(ctx)
		endpoint := info.FullMethod

		if !rl.allowRequest(clientID, endpoint) {
			return nil, status.Errorf(codes.ResourceExhausted, "rate limit exceeded. Please try again later.")
		}

		return handler(ctx, req)
	}
}

// extractClientID extracts a client identifier from the context
// Uses IP address or metadata-based identification
func (rl *RateLimiter) extractClientID(ctx context.Context) string {
	// Try to get from peer info (IP address)
	if p, ok := peer.FromContext(ctx); ok {
		if addr, ok := p.Addr.(*net.TCPAddr); ok {
			return addr.IP.String()
		}
		return p.Addr.String()
	}

	// Fallback to metadata
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if apiKeys := md.Get("x-api-key"); len(apiKeys) > 0 {
			return "apikey:" + apiKeys[0]
		}
		if tokens := md.Get("authorization"); len(tokens) > 0 {
			// Use token prefix as identifier
			token := tokens[0]
			if len(token) > 20 {
				return "token:" + token[:20]
			}
			return "token:" + token
		}
	}

	return "unknown"
}

// allowRequest checks if a request should be allowed based on rate limits
func (rl *RateLimiter) allowRequest(clientID, endpoint string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Get or create client map
	if rl.requests[clientID] == nil {
		rl.requests[clientID] = make(map[string][]time.Time)
	}

	// Get limit for this endpoint
	limit := rl.defaultLimit
	if l, ok := rl.limits[endpoint]; ok {
		limit = l
	}

	// Clean old requests outside the window
	requests := rl.requests[clientID][endpoint]
	var validRequests []time.Time
	windowStart := now.Add(-limit.Window)
	for _, t := range requests {
		if t.After(windowStart) {
			validRequests = append(validRequests, t)
		}
	}

	// Check if under limit
	if len(validRequests) >= limit.Requests {
		return false
	}

	// Add current request
	validRequests = append(validRequests, now)
	rl.requests[clientID][endpoint] = validRequests

	return true
}

// Cleanup removes old entries periodically (call in a goroutine)
func (rl *RateLimiter) Cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		maxWindow := rl.defaultLimit.Window
		for _, limit := range rl.limits {
			if limit.Window > maxWindow {
				maxWindow = limit.Window
			}
		}
		cutoff := now.Add(-maxWindow * 2)

		for clientID, endpoints := range rl.requests {
			for endpoint, requests := range endpoints {
				var valid []time.Time
				for _, t := range requests {
					if t.After(cutoff) {
						valid = append(valid, t)
					}
				}
				if len(valid) == 0 {
					delete(rl.requests[clientID], endpoint)
				} else {
					rl.requests[clientID][endpoint] = valid
				}
			}
			if len(rl.requests[clientID]) == 0 {
				delete(rl.requests, clientID)
			}
		}
		rl.mu.Unlock()
	}
}

// HTTPRateLimiter provides rate limiting for HTTP endpoints
type HTTPRateLimiter struct {
	limiter   *RateLimiter
	endpoints map[string]RateLimit // path -> limit
}

// NewHTTPRateLimiter creates a rate limiter for HTTP endpoints
func NewHTTPRateLimiter() *HTTPRateLimiter {
	return &HTTPRateLimiter{
		limiter:   NewRateLimiter(),
		endpoints: make(map[string]RateLimit),
	}
}

// SetEndpointLimit sets rate limit for a specific HTTP endpoint pattern
func (hrl *HTTPRateLimiter) SetEndpointLimit(pattern string, requests int, window time.Duration) {
	hrl.endpoints[pattern] = RateLimit{
		Requests: requests,
		Window:   window,
	}
}

// Middleware returns an HTTP middleware for rate limiting
func (hrl *HTTPRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientID := hrl.extractClientID(r)
		path := r.URL.Path

		// Find matching endpoint pattern
		limit := hrl.findLimit(path)

		if !hrl.limiter.allowHTTPRequest(clientID, path, limit) {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(limit.Window.Seconds())))
			http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (hrl *HTTPRateLimiter) extractClientID(r *http.Request) string {
	// Use X-Forwarded-For or RemoteAddr
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	if ip != "" {
		return ip
	}

	// Fallback to API key
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		return "apikey:" + apiKey
	}

	// Fallback to auth token prefix
	if auth := r.Header.Get("Authorization"); auth != "" {
		token := strings.TrimPrefix(auth, "Bearer ")
		if len(token) > 20 {
			return "token:" + token[:20]
		}
		return "token:" + token
	}

	return "unknown"
}

func (hrl *HTTPRateLimiter) findLimit(path string) RateLimit {
	for pattern, limit := range hrl.endpoints {
		if strings.Contains(path, pattern) {
			return limit
		}
	}
	return RateLimit{Requests: 100, Window: time.Minute} // Default
}

// allowHTTPRequest checks if an HTTP request should be allowed
func (rl *RateLimiter) allowHTTPRequest(clientID, path string, limit RateLimit) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Get or create client map
	if rl.requests[clientID] == nil {
		rl.requests[clientID] = make(map[string][]time.Time)
	}

	// Clean old requests
	requests := rl.requests[clientID][path]
	var validRequests []time.Time
	windowStart := now.Add(-limit.Window)
	for _, t := range requests {
		if t.After(windowStart) {
			validRequests = append(validRequests, t)
		}
	}

	// Check if under limit
	if len(validRequests) >= limit.Requests {
		return false
	}

	// Add current request
	validRequests = append(validRequests, now)
	rl.requests[clientID][path] = validRequests

	return true
}
