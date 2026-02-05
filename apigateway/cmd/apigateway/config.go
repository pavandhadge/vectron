// This file defines the configuration for the API Gateway service.
// It includes settings for server addresses, connection to the placement driver,
// and security parameters like JWT secret and rate limiting.

package main

import (
	"os"
	"strconv"
	"strings"
)

// Config holds all the configuration settings for the API Gateway.
type Config struct {
	GRPCAddr                   string          // Address for the gRPC server to listen on.
	HTTPAddr                   string          // Address for the HTTP/JSON gateway to listen on.
	PlacementDriver            string          // Address of the placement driver service.
	JWTSecret                  string          // Secret key for signing and verifying JWT tokens.
	AuthServiceAddr            string          // Address of the Auth service.
	RerankerServiceAddr        string          // Address of the Reranker service.
	FeedbackDBPath             string          // Path to the feedback SQLite database.
	RateLimitRPS               int             // Requests per second for the rate limiter.
	SearchLinearizable         bool            // Whether search reads should be linearizable.
	RerankTimeoutMs            int             // Timeout for reranking (milliseconds).
	RerankEnabled              bool            // Global rerank enable switch.
	RerankCollections          map[string]bool // Collection allowlist for reranking (empty = allow all when enabled).
	RerankTopNOverrides        map[string]int  // Per-collection TopN overrides.
	RerankTimeoutOverrides     map[string]int  // Per-collection timeout overrides (ms).
	GRPCEnableCompression      bool            // Enable gzip compression for gRPC clients.
	RerankWarmupEnabled        bool            // Enable async rerank warmup for recurring queries.
	RerankWarmupTTLms          int             // TTL for warmup rerank cache (milliseconds).
	RerankWarmupMaxSize        int             // Max entries for warmup rerank cache.
	RerankWarmupConcurrency    int             // Max concurrent warmup rerank calls.
	SearchConsistencyOverrides map[string]bool // Per-collection read consistency (true = linearizable).
}

// LoadConfig loads the configuration from environment variables with default fallbacks.
func LoadConfig() Config {
	return Config{
		GRPCAddr:                   getEnv("GRPC_ADDR", ":8081"),
		HTTPAddr:                   getEnv("HTTP_ADDR", ":8080"),
		PlacementDriver:            getEnv("PLACEMENT_DRIVER", "placement:6300"),
		JWTSecret:                  getEnv("JWT_SECRET", "CHANGE_ME_IN_PRODUCTION"),
		AuthServiceAddr:            getEnv("AUTH_SERVICE_ADDR", "auth:50051"),          // Default Auth service address
		RerankerServiceAddr:        getEnv("RERANKER_SERVICE_ADDR", "localhost:50051"), // Default Reranker service address
		FeedbackDBPath:             getEnv("FEEDBACK_DB_PATH", "./data/feedback.db"),   // Default feedback database path
		RateLimitRPS:               getEnvAsInt("RATE_LIMIT_RPS", 100),
		SearchLinearizable:         getEnvAsBool("SEARCH_LINEARIZABLE", false),
		RerankTimeoutMs:            getEnvAsInt("RERANK_TIMEOUT_MS", 75),
		RerankEnabled:              getEnvAsBool("RERANK_ENABLED", false),
		RerankCollections:          parseBoolSet(getEnv("RERANK_COLLECTIONS", "")),
		RerankTopNOverrides:        parseKVIntMap(getEnv("RERANK_TOP_N_OVERRIDES", "")),
		RerankTimeoutOverrides:     parseKVIntMap(getEnv("RERANK_TIMEOUT_OVERRIDES", "")),
		GRPCEnableCompression:      getEnvAsBool("GRPC_ENABLE_COMPRESSION", false),
		RerankWarmupEnabled:        getEnvAsBool("RERANK_WARMUP_ENABLED", false),
		RerankWarmupTTLms:          getEnvAsInt("RERANK_WARMUP_TTL_MS", 30000),
		RerankWarmupMaxSize:        getEnvAsInt("RERANK_WARMUP_MAX_SIZE", 2000),
		RerankWarmupConcurrency:    getEnvAsInt("RERANK_WARMUP_CONCURRENCY", 2),
		SearchConsistencyOverrides: parseConsistencyOverrides(getEnv("SEARCH_CONSISTENCY_OVERRIDES", "")),
	}
}

// getEnv retrieves an environment variable by key, returning a fallback value if not set.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvAsInt retrieves an environment variable as an integer, returning a fallback value if not set or invalid.
func getEnvAsInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

// getEnvAsBool retrieves an environment variable as a boolean, returning a fallback value if not set or invalid.
func getEnvAsBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

// parseBoolSet parses comma-separated values into a set.
func parseBoolSet(value string) map[string]bool {
	set := make(map[string]bool)
	if value == "" {
		return set
	}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		set[item] = true
	}
	return set
}

// parseKVIntMap parses "key=value,key2=value2" into a map.
func parseKVIntMap(value string) map[string]int {
	out := make(map[string]int)
	if value == "" {
		return out
	}
	parts := strings.Split(value, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if key == "" || val == "" {
			continue
		}
		if i, err := strconv.Atoi(val); err == nil {
			out[key] = i
		}
	}
	return out
}

// parseConsistencyOverrides parses "collection=linearizable,other=eventual" into a map.
func parseConsistencyOverrides(value string) map[string]bool {
	out := make(map[string]bool)
	if value == "" {
		return out
	}
	parts := strings.Split(value, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if key == "" || val == "" {
			continue
		}
		switch strings.ToLower(val) {
		case "linearizable", "strong", "true", "1":
			out[key] = true
		case "eventual", "stale", "false", "0":
			out[key] = false
		}
	}
	return out
}
