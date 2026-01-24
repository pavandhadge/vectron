// Package internal provides the core reranker service implementation.
// It implements a pluggable strategy pattern for different reranking approaches
// (rule-based, LLM-based, RL-based) with caching and performance monitoring.
package internal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Strategy defines the interface that all reranking implementations must satisfy.
// This allows for plug-and-play swapping between rule-based, LLM, and RL approaches.
type Strategy interface {
	// Name returns the strategy identifier (e.g., "rule-based", "llm", "rl")
	Name() string

	// Version returns the strategy version for tracking and A/B testing
	Version() string

	// Rerank takes a query and candidates, returns scored and reordered results
	Rerank(ctx context.Context, req *RerankInput) (*RerankOutput, error)

	// Config returns strategy-specific configuration as key-value pairs
	Config() map[string]string
}

// RerankInput represents the input to a reranking strategy.
type RerankInput struct {
	Query      string
	Candidates []Candidate
	TopN       int
	Options    map[string]string
}

// Candidate represents a single search result to be reranked.
type Candidate struct {
	ID       string
	Score    float32 // Original similarity score
	Vector   []float32
	Metadata map[string]string
}

// RerankOutput represents the output from a reranking strategy.
type RerankOutput struct {
	Results   []ScoredCandidate
	Latency   time.Duration
	Metadata  map[string]string // Strategy-specific metadata
}

// ScoredCandidate represents a candidate with its reranking score.
type ScoredCandidate struct {
	ID            string
	RerankScore   float32
	OriginalScore float32
	Explanation   string
}

// Cache defines the interface for caching reranking results.
type Cache interface {
	// Get retrieves a cached result by key
	Get(key string) (*RerankOutput, bool)

	// Set stores a result in cache with optional TTL
	Set(key string, value *RerankOutput, ttl time.Duration)

	// Delete removes a specific cache entry
	Delete(key string)

	// Clear removes all cache entries (or matching a pattern)
	Clear(pattern string) int
}

// CacheKey generates a deterministic cache key from query and candidate IDs.
func CacheKey(query string, candidateIDs []string) string {
	// Sort IDs for deterministic key generation
	sorted := make([]string, len(candidateIDs))
	copy(sorted, candidateIDs)
	sort.Strings(sorted)

	// Hash the query + sorted IDs
	data := query + "|" + strings.Join(sorted, ",")
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// StrategyFactory creates strategy instances based on configuration.
type StrategyFactory interface {
	Create(strategyType string, config map[string]string) (Strategy, error)
}

// MetricsCollector defines the interface for collecting performance metrics.
type MetricsCollector interface {
	RecordLatency(strategy string, latency time.Duration)
	RecordCacheHit(strategy string)
	RecordCacheMiss(strategy string)
	RecordError(strategy string, errType string)
}

// Logger defines the interface for structured logging.
type Logger interface {
	Info(msg string, fields map[string]interface{})
	Error(msg string, err error, fields map[string]interface{})
	Debug(msg string, fields map[string]interface{})
}

// StrategyConfig holds configuration for a reranking strategy.
type StrategyConfig struct {
	Name    string
	Type    string // "rule", "llm", "rl"
	Enabled bool
	Options map[string]string
	Cache   CacheConfig
}

// CacheConfig holds cache-specific settings.
type CacheConfig struct {
	Enabled bool
	TTL     time.Duration
	Backend string // "memory", "redis"
	MaxSize int
}

// ValidationError represents invalid input errors.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error on field '%s': %s", e.Field, e.Message)
}

// ValidateRerankInput checks if the input is valid.
func ValidateRerankInput(input *RerankInput) error {
	if input.Query == "" {
		return &ValidationError{Field: "query", Message: "query cannot be empty"}
	}
	if len(input.Candidates) == 0 {
		return &ValidationError{Field: "candidates", Message: "candidates list is empty"}
	}
	if input.TopN <= 0 {
		return &ValidationError{Field: "top_n", Message: "top_n must be positive"}
	}
	if input.TopN > len(input.Candidates) {
		input.TopN = len(input.Candidates) // Auto-adjust
	}
	return nil
}
