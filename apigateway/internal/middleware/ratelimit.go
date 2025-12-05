package middleware

import (
	"net/http"
	"sync"
	"time"
)

type limiter struct {
	count   int
	resetAt time.Time
}

var (
	mu     sync.Mutex
	limits = make(map[string]*limiter)
)

func RateLimit(rps int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value(UserIDKey).(string)
			if !ok {
				http.Error(w, `{"error":"auth required"}`, http.StatusUnauthorized)
				return
			}

			mu.Lock()
			l := limits[userID]
			now := time.Now()

			if l == nil || now.After(l.resetAt) {
				l = &limiter{count: 0, resetAt: now.Add(time.Second)}
				limits[userID] = l
			}

			if l.count >= rps {
				w.Header().Set("X-RateLimit-Reset", l.resetAt.Format(time.RFC3339))
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				mu.Unlock()
				return
			}

			l.count++
			mu.Unlock()

			w.Header().Set("X-RateLimit-Remaining", string(rune(rps-l.count)))
			next.ServeHTTP(w, r)
		})
	}
}
