package middleware

import (
	"log"
	"net/http"
	"time"
)

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		userID, _ := r.Context().Value(UserIDKey).(string)
		log.Printf("[gateway] %s %s user=%s", r.Method, r.URL.Path, userID)
		next.ServeHTTP(w, r)
		log.Printf("[gateway] completed in %v", time.Since(start))
	})
}
