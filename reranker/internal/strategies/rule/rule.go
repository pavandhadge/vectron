// Package rule implements rule-based reranking strategies.
// It combines multiple heuristics like keyword matching, TF-IDF scoring,
// metadata boosting, and configurable penalties to enhance search relevance.
package rule

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pavandhadge/vectron/reranker/internal"
)

// Strategy implements rule-based reranking using configurable heuristics.
type Strategy struct {
	config        Config
	tokenizeRegex *regexp.Regexp
	stopWordsSet  map[string]bool
}

// Config holds the configuration for rule-based reranking.
type Config struct {
	// Keyword matching weights
	ExactMatchBoost float32 // Boost for exact keyword matches (default: 0.3)
	FuzzyMatchBoost float32 // Boost for fuzzy/partial matches (default: 0.1)
	TitleBoost      float32 // Boost for matches in title field (default: 0.2)

	// Metadata rules
	MetadataBoosts    map[string]float32 // Boost by metadata key-value
	MetadataPenalties map[string]float32 // Penalize by metadata key-value

	// Content quality signals
	RecencyBoost float32 // Boost recent documents (default: 0.15)
	RecencyField string  // Metadata field for timestamp (default: "created_at")
	RecencyDays  int     // Days to consider "recent" (default: 30)

	// Scoring parameters
	TFIDFWeight    float32 // Weight for TF-IDF score (default: 0.4)
	OriginalWeight float32 // Weight for original vector score (default: 0.6)

	// Advanced
	StopWords     []string // Words to ignore in keyword matching
	CaseSensitive bool     // Whether keyword matching is case-sensitive
}

// DefaultConfig returns a reasonable default configuration.
func DefaultConfig() Config {
	return Config{
		ExactMatchBoost:   0.3,
		FuzzyMatchBoost:   0.1,
		TitleBoost:        0.2,
		MetadataBoosts:    make(map[string]float32),
		MetadataPenalties: make(map[string]float32),
		RecencyBoost:      0.15,
		RecencyField:      "created_at",
		RecencyDays:       30,
		TFIDFWeight:       0.4,
		OriginalWeight:    0.6,
		StopWords: []string{
			"the", "a", "an", "and", "or", "but", "in", "on", "at", "to", "for",
			"of", "with", "is", "was", "are", "were", "be", "been", "being",
		},
		CaseSensitive: false,
	}
}

// NewStrategy creates a new rule-based reranking strategy.
func NewStrategy(config Config) *Strategy {
	// Pre-compile expensive operations
	stopWordsSet := make(map[string]bool, len(config.StopWords))
	for _, sw := range config.StopWords {
		stopWordsSet[strings.ToLower(sw)] = true
	}

	return &Strategy{
		config:        config,
		tokenizeRegex: regexp.MustCompile(`[^\w\s]+`),
		stopWordsSet:  stopWordsSet,
	}
}

// Name returns the strategy identifier.
func (s *Strategy) Name() string {
	return "rule-based"
}

// Version returns the strategy version.
func (s *Strategy) Version() string {
	return "1.0.0"
}

// Config returns the strategy configuration as key-value pairs.
func (s *Strategy) Config() map[string]string {
	return map[string]string{
		"exact_match_boost": floatToString(s.config.ExactMatchBoost),
		"fuzzy_match_boost": floatToString(s.config.FuzzyMatchBoost),
		"title_boost":       floatToString(s.config.TitleBoost),
		"tfidf_weight":      floatToString(s.config.TFIDFWeight),
		"original_weight":   floatToString(s.config.OriginalWeight),
		"recency_days":      intToString(s.config.RecencyDays),
	}
}

// Rerank applies rule-based scoring to reorder candidates.
func (s *Strategy) Rerank(ctx context.Context, req *internal.RerankInput) (*internal.RerankOutput, error) {
	start := time.Now()

	// If query is empty, apply universal rules: sort by original score and return.
	if req.Query == "" {
		sort.Slice(req.Candidates, func(i, j int) bool {
			return req.Candidates[i].Score > req.Candidates[j].Score
		})

		results := make([]internal.ScoredCandidate, 0, req.TopN)
		for i, candidate := range req.Candidates {
			if i >= req.TopN {
				break
			}
			results = append(results, internal.ScoredCandidate{
				ID:            candidate.ID,
				RerankScore:   candidate.Score,    // Use original score as rerank score
				OriginalScore: candidate.Score,
				Explanation:   "no query provided, sorted by original score",
			})
		}
		return &internal.RerankOutput{
			Results: results,
			Latency: time.Since(start),
			Metadata: map[string]string{
				"strategy":    s.Name(),
				"version":     s.Version(),
				"query_empty": "true",
			},
		}, nil
	}

	// Tokenize query
	queryTokens := s.tokenize(req.Query)

	// Calculate document frequencies for TF-IDF
	docFreqs := s.calculateDocumentFrequencies(req.Candidates, queryTokens)

	// Score each candidate
	scored := make([]scoredCandidate, len(req.Candidates))
	for i, candidate := range req.Candidates {
		score := s.scoreCandidate(candidate, queryTokens, docFreqs, len(req.Candidates))
		scored[i] = scoredCandidate{
			candidate: candidate,
			score:     score,
		}
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Limit to TopN
	if req.TopN < len(scored) {
		scored = scored[:req.TopN]
	}

	// Convert to output format
	results := make([]internal.ScoredCandidate, len(scored))
	for i, sc := range scored {
		results[i] = internal.ScoredCandidate{
			ID:            sc.candidate.ID,
			RerankScore:   sc.score,
			OriginalScore: sc.candidate.Score,
			Explanation:   s.explainScore(sc.candidate, queryTokens, sc.score),
		}
	}

	return &internal.RerankOutput{
		Results: results,
		Latency: time.Since(start),
		Metadata: map[string]string{
			"strategy": s.Name(),
			"version":  s.Version(),
		},
	}, nil
}

// scoredCandidate is an internal struct for sorting.
type scoredCandidate struct {
	candidate internal.Candidate
	score     float32
}

// scoreCandidate computes the reranking score for a single candidate.
func (s *Strategy) scoreCandidate(candidate internal.Candidate, queryTokens []string, docFreqs map[string]int, totalDocs int) float32 {
	// Start with original similarity score
	score := s.config.OriginalWeight * candidate.Score

	// Add TF-IDF score
	tfidfScore := s.calculateTFIDF(candidate, queryTokens, docFreqs, totalDocs)
	score += s.config.TFIDFWeight * tfidfScore

	// Add keyword match boosts
	score += s.calculateKeywordBoosts(candidate, queryTokens)

	// Add metadata boosts/penalties
	score += s.calculateMetadataScore(candidate)

	// Add recency boost
	score += s.calculateRecencyBoost(candidate)

	// Normalize to 0-1 range
	return clamp(score, 0.0, 1.0)
}

// calculateTFIDF computes TF-IDF score for query terms in candidate content.
func (s *Strategy) calculateTFIDF(candidate internal.Candidate, queryTokens []string, docFreqs map[string]int, totalDocs int) float32 {
	// Concatenate all text fields from metadata
	content := s.getCandidateText(candidate)
	contentTokens := s.tokenize(content)

	// Calculate term frequencies in document
	termFreqs := make(map[string]int)
	for _, token := range contentTokens {
		termFreqs[token]++
	}

	var tfidfSum float32
	for _, queryToken := range queryTokens {
		if tf, exists := termFreqs[queryToken]; exists && tf > 0 {
			// TF (term frequency): normalized by document length
			tfScore := float32(tf) / float32(len(contentTokens))

			// IDF (inverse document frequency)
			df := docFreqs[queryToken]
			if df == 0 {
				df = 1 // Avoid division by zero
			}
			idfScore := float32(math.Log(float64(totalDocs) / float64(df)))

			tfidfSum += tfScore * idfScore
		}
	}

	// Normalize by number of query terms
	if len(queryTokens) > 0 {
		return tfidfSum / float32(len(queryTokens))
	}
	return 0
}

// calculateKeywordBoosts checks for exact and fuzzy matches.
func (s *Strategy) calculateKeywordBoosts(candidate internal.Candidate, queryTokens []string) float32 {
	content := s.getCandidateText(candidate)
	title := candidate.Metadata["title"]

	var boost float32

	query := strings.Join(queryTokens, " ")

	// Exact match in content
	if s.containsIgnoreCase(content, query) {
		boost += s.config.ExactMatchBoost
	}

	// Exact match in title (higher weight)
	if title != "" && s.containsIgnoreCase(title, query) {
		boost += s.config.TitleBoost
	}

	// Fuzzy matching: count individual token matches
	contentLower := strings.ToLower(content)
	matchCount := 0
	for _, token := range queryTokens {
		if strings.Contains(contentLower, strings.ToLower(token)) {
			matchCount++
		}
	}

	if matchCount > 0 && len(queryTokens) > 0 {
		matchRatio := float32(matchCount) / float32(len(queryTokens))
		boost += s.config.FuzzyMatchBoost * matchRatio
	}

	return boost
}

// calculateMetadataScore applies configured boosts and penalties.
func (s *Strategy) calculateMetadataScore(candidate internal.Candidate) float32 {
	var score float32

	// Apply boosts
	for key, boostValue := range s.config.MetadataBoosts {
		if value, exists := candidate.Metadata[key]; exists && value != "" {
			// Check if it's a boolean "true" or just presence
			if value == "true" || value == "1" {
				score += boostValue
			}
		}
	}

	// Apply penalties
	for key, penaltyValue := range s.config.MetadataPenalties {
		if value, exists := candidate.Metadata[key]; exists && value != "" {
			if value == "true" || value == "1" {
				score -= penaltyValue
			}
		}
	}

	return score
}

// calculateRecencyBoost boosts recent documents based on timestamp.
func (s *Strategy) calculateRecencyBoost(candidate internal.Candidate) float32 {
	if s.config.RecencyBoost == 0 || s.config.RecencyField == "" {
		return 0
	}

	timestampStr, exists := candidate.Metadata[s.config.RecencyField]
	if !exists || timestampStr == "" {
		return 0
	}

	// Try parsing as RFC3339 timestamp
	timestamp, err := time.Parse(time.RFC3339, timestampStr)
	if err != nil {
		// Try Unix timestamp
		return 0 // Skip if can't parse
	}

	daysSince := time.Since(timestamp).Hours() / 24
	if daysSince < float64(s.config.RecencyDays) {
		// Linear decay: full boost at day 0, zero at RecencyDays
		decayFactor := 1.0 - (daysSince / float64(s.config.RecencyDays))
		return s.config.RecencyBoost * float32(decayFactor)
	}

	return 0
}

// calculateDocumentFrequencies counts how many docs contain each query term.
func (s *Strategy) calculateDocumentFrequencies(candidates []internal.Candidate, queryTokens []string) map[string]int {
	docFreqs := make(map[string]int)

	for _, token := range queryTokens {
		for _, candidate := range candidates {
			content := s.getCandidateText(candidate)
			if s.containsIgnoreCase(content, token) {
				docFreqs[token]++
			}
		}
	}

	return docFreqs
}

// getCandidateText extracts all text content from a candidate.
func (s *Strategy) getCandidateText(candidate internal.Candidate) string {
	var parts []string

	// Common text fields
	if title := candidate.Metadata["title"]; title != "" {
		parts = append(parts, title)
	}
	if content := candidate.Metadata["content"]; content != "" {
		parts = append(parts, content)
	}
	if description := candidate.Metadata["description"]; description != "" {
		parts = append(parts, description)
	}
	if tags := candidate.Metadata["tags"]; tags != "" {
		parts = append(parts, tags)
	}

	return strings.Join(parts, " ")
}

// tokenize splits text into tokens, removing stop words.
// Optimized to use pre-compiled regex and stop words set.
func (s *Strategy) tokenize(text string) []string {
	// Convert to lowercase unless case-sensitive
	if !s.config.CaseSensitive {
		text = strings.ToLower(text)
	}

	// Remove punctuation and split on whitespace using pre-compiled regex
	text = s.tokenizeRegex.ReplaceAllString(text, " ")

	words := strings.Fields(text)

	// Filter out stop words using pre-built set (much faster than rebuilding)
	tokens := make([]string, 0, len(words))
	for _, word := range words {
		if !s.stopWordsSet[strings.ToLower(word)] && len(word) > 1 {
			tokens = append(tokens, word)
		}
	}

	return tokens
}

// containsIgnoreCase checks if text contains substring (case-insensitive).
func (s *Strategy) containsIgnoreCase(text, substr string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(substr))
}

// explainScore generates a human-readable explanation of the score.
func (s *Strategy) explainScore(candidate internal.Candidate, queryTokens []string, finalScore float32) string {
	reasons := []string{}

	content := s.getCandidateText(candidate)

	// Check for exact matches
	query := strings.Join(queryTokens, " ")
	if s.containsIgnoreCase(content, query) {
		reasons = append(reasons, "exact query match")
	}

	// Check title match
	if title := candidate.Metadata["title"]; title != "" && s.containsIgnoreCase(title, query) {
		reasons = append(reasons, "query in title")
	}

	// Check metadata boosts
	for key := range s.config.MetadataBoosts {
		if value, exists := candidate.Metadata[key]; exists && (value == "true" || value == "1") {
			reasons = append(reasons, "boosted: "+key)
		}
	}

	// Check recency
	if timestampStr, exists := candidate.Metadata[s.config.RecencyField]; exists && timestampStr != "" {
		reasons = append(reasons, "recent document")
	}

	if len(reasons) == 0 {
		return "keyword relevance"
	}

	return strings.Join(reasons, ", ")
}

// Helper functions
func clamp(value, min, max float32) float32 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func floatToString(f float32) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", f), "0"), ".")
}

func intToString(i int) string {
	return fmt.Sprintf("%d", i)
}
