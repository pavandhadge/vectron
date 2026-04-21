// Package rule implements rule-based reranking strategies.
// It combines BM25 scoring, field-weighted boosting, and temporal signals
// for enhanced search relevance.
package rule

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pavandhadge/vectron/reranker/internal"
)

// Strategy implements rule-based reranking using improved heuristics.
type Strategy struct {
	config        Config
	tokenizeRegex *regexp.Regexp
	stopWordsSet map[string]bool
	k1          float32
	b            float32
}

// Config holds the configuration for rule-based reranking.
type Config struct {
	FieldWeights FieldWeightConfig

	BM25K1         float32
	BM25B           float32

	OriginalWeight float32
	BM25Weight     float32
	KeywordWeight float32
	CoverageWeight float32
	RecencyWeight float32

	ExactMatchBoost float32
	TitleBoost    float32
	FuzzyBoost    float32

	MetadataBoosts    map[string]float32
	MetadataPenalties map[string]float32

	RecencyField  string
	RecencyDays  int
	HalfLifeDays float64

	StopWords     []string
	CaseSensitive bool
}

// FieldWeightConfig defines field weights for field-weighted scoring.
type FieldWeightConfig struct {
	Title       float32
	Description float32
	Tags        float32
	Content     float32
}

func DefaultConfig() Config {
	return Config{
		FieldWeights: FieldWeightConfig{
			Title:       3.0,
			Description: 2.0,
			Tags:        1.5,
			Content:     1.0,
		},
		BM25K1:         1.2,
		BM25B:          0.75,
		OriginalWeight: 0.5,
		BM25Weight:     0.25,
		KeywordWeight:  0.15,
		CoverageWeight:  0.05,
		RecencyWeight:  0.05,
		ExactMatchBoost: 0.3,
		TitleBoost:     0.2,
		FuzzyBoost:     0.05,
		MetadataBoosts:    make(map[string]float32),
		MetadataPenalties: make(map[string]float32),
		RecencyField:  "created_at",
		RecencyDays:  30,
		HalfLifeDays: 7,
		StopWords: []string{
			"the", "a", "an", "and", "or", "but", "in", "on", "at", "to", "for",
			"of", "with", "is", "was", "are", "were", "be", "been", "being",
		},
		CaseSensitive: false,
	}
}

func NewStrategy(config Config) *Strategy {
	stopWordsSet := make(map[string]bool, len(config.StopWords))
	for _, sw := range config.StopWords {
		stopWordsSet[strings.ToLower(sw)] = true
	}
	return &Strategy{
		config:        config,
		tokenizeRegex: regexp.MustCompile(`[^\w\s]+`),
		stopWordsSet: stopWordsSet,
		k1:           config.BM25K1,
		b:            config.BM25B,
	}
}

func (s *Strategy) Name() string              { return "rule-based" }
func (s *Strategy) Version() string         { return "2.0.0" }
func (s *Strategy) NeedsCandidateVectors() bool { return false }

func (s *Strategy) Config() map[string]string {
	return map[string]string{
		"original_weight": fmt.Sprintf("%.2f", s.config.OriginalWeight),
		"bm25_weight":     fmt.Sprintf("%.2f", s.config.BM25Weight),
		"keyword_weight":  fmt.Sprintf("%.2f", s.config.KeywordWeight),
		"coverage_weight": fmt.Sprintf("%.2f", s.config.CoverageWeight),
		"recency_weight":  fmt.Sprintf("%.2f", s.config.RecencyWeight),
		"half_life_days":   fmt.Sprintf("%.1f", s.config.HalfLifeDays),
	}
}

func (s *Strategy) Rerank(ctx context.Context, req *internal.RerankInput) (*internal.RerankOutput, error) {
	start := time.Now()

	if req.Query == "" {
		return s.rerankByOriginalScore(ctx, req)
	}

	queryTokens := s.tokenize(req.Query)
	if len(queryTokens) == 0 {
		return s.rerankByOriginalScore(ctx, req)
	}

	docs := s.extractFields(req.Candidates)
	avgDL := s.calculateAverageDocLength(docs)
	docFreqs := s.calculateDocumentFrequencies(docs, queryTokens)

	var candidates []candidateScore
	for i, c := range req.Candidates {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		doc := docs[i]
		score := s.calculateScore(c, doc, queryTokens, docFreqs, avgDL, len(req.Candidates))
		candidates = append(candidates, candidateScore{
			ID:         c.ID,
			Score:      score,
			Original:   c.Score,
			HasExact:   s.hasExactMatch(doc, queryTokens),
			HasTitle:   s.hasTitleMatch(doc, queryTokens),
			MatchCount: s.countMatches(doc, queryTokens),
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].Score > candidates[j].Score
	})

	topN := int(req.TopN)
	if topN > len(candidates) {
		topN = len(candidates)
	}

	results := make([]internal.ScoredCandidate, topN)
	for i := 0; i < topN; i++ {
		c := candidates[i]
		results[i] = internal.ScoredCandidate{
			ID:            c.ID,
			RerankScore:   c.Score,
			OriginalScore: c.Original,
			Explanation:  s.explainScore(c),
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

type candidateScore struct {
	ID         string
	Score      float32
	Original   float32
	HasExact   bool
	HasTitle   bool
	MatchCount int
}

type documentFields struct {
	Title      string
	AllText    string
	TitleLower string
	AllLower  string
	Tokens    []string
	TokenSet  map[string]bool
	Length    int
	Metadata  map[string]string
}

func (s *Strategy) extractFields(candidates []internal.Candidate) []documentFields {
	docs := make([]documentFields, len(candidates))
	for i, c := range candidates {
		m := c.Metadata
		title := s.getField(m, "title")
		desc := s.getField(m, "description")
		tags := s.getField(m, "tags")
		content := s.getField(m, "content")

		text := strings.Join([]string{title, desc, tags, content}, " ")
		textLower := strings.ToLower(text)

		tokens := s.tokenize(text)
		tokenSet := make(map[string]bool, len(tokens))
		for _, t := range tokens {
			tokenSet[t] = true
		}

		docs[i] = documentFields{
			Title:      title,
			AllText:    text,
			TitleLower: strings.ToLower(title),
			AllLower:   textLower,
			Tokens:    tokens,
			TokenSet:  tokenSet,
			Length:    len(tokens),
			Metadata:  m,
		}
	}
	return docs
}

func (s *Strategy) getField(m map[string]string, key string) string {
	if v, ok := m[key]; ok {
		return v
	}
	return ""
}

func (s *Strategy) tokenize(text string) []string {
	if !s.config.CaseSensitive {
		text = strings.ToLower(text)
	}
	text = s.tokenizeRegex.ReplaceAllString(text, " ")
	words := strings.Fields(text)

	tokens := make([]string, 0, len(words))
	for _, word := range words {
		if !s.stopWordsSet[word] && len(word) > 1 {
			tokens = append(tokens, word)
		}
	}
	return tokens
}

func (s *Strategy) rerankByOriginalScore(ctx context.Context, req *internal.RerankInput) (*internal.RerankOutput, error) {
	start := time.Now()

	sort.Slice(req.Candidates, func(i, j int) bool {
		return req.Candidates[i].Score > req.Candidates[j].Score
	})

	topN := int(req.TopN)
	if topN > len(req.Candidates) {
		topN = len(req.Candidates)
	}

	results := make([]internal.ScoredCandidate, topN)
	for i := 0; i < topN; i++ {
		c := req.Candidates[i]
		results[i] = internal.ScoredCandidate{
			ID:            c.ID,
			RerankScore:   c.Score,
			OriginalScore: c.Score,
			Explanation:  "sorted by original score",
		}
	}

	return &internal.RerankOutput{
		Results: results,
		Latency: time.Since(start),
		Metadata: map[string]string{
			"strategy":    s.Name(),
			"query_empty": "true",
		},
	}, nil
}

func (s *Strategy) calculateAverageDocLength(docs []documentFields) float32 {
	if len(docs) == 0 {
		return 0
	}
	var total int
	for _, d := range docs {
		total += d.Length
	}
	return float32(total) / float32(len(docs))
}

func (s *Strategy) calculateDocumentFrequencies(docs []documentFields, queryTokens []string) map[string]int {
	docFreqs := make(map[string]int)
	for _, token := range queryTokens {
		count := 0
		for _, doc := range docs {
			if doc.TokenSet[token] {
				count++
			}
		}
		docFreqs[token] = count
	}
	return docFreqs
}

func (s *Strategy) calculateScore(candidate internal.Candidate, doc documentFields, queryTokens []string, docFreqs map[string]int, avgDL float32, N int) float32 {
	var score float32

	score += s.config.OriginalWeight * candidate.Score

	bm25Score := s.calculateBM25(doc, queryTokens, docFreqs, avgDL, N)
	score += s.config.BM25Weight * bm25Score

	keywordScore := s.calculateKeywordScore(doc, queryTokens)
	score += s.config.KeywordWeight * keywordScore

	coverageScore := s.calculateCoverage(doc, queryTokens)
	score += s.config.CoverageWeight * coverageScore

	recencyScore := s.calculateRecency(doc)
	score += s.config.RecencyWeight * recencyScore

	metaScore := s.calculateMetadataScore(doc)
	score += metaScore

	return clamp(score, 0.0, 1.0)
}

func (s *Strategy) calculateBM25(doc documentFields, queryTokens []string, docFreqs map[string]int, avgDL float32, N int) float32 {
	if doc.Length == 0 || len(queryTokens) == 0 {
		return 0
	}

	var bm25 float32
	fw := s.config.FieldWeights

	for _, token := range queryTokens {
		tf := 1.0

		if strings.Contains(doc.TitleLower, token) {
			tf *= float64(fw.Title)
		} else if strings.Contains(doc.AllLower, token) {
			tf *= float64(fw.Content)
		}

		df := docFreqs[token]
		if df == 0 {
			df = 1
		}

		idf := math.Log(float64(N)/float64(df) + 1)
		tfNorm := float32(tf * (float64(s.k1) * (1 - float64(s.b) + float64(s.b)*float64(doc.Length)/float64(avgDL))))
		bm25 += float32(idf) * (tfNorm / (float32(tfNorm) + tfNorm))
	}

	if len(queryTokens) > 0 {
		return bm25 / float32(len(queryTokens))
	}
	return 0
}

func (s *Strategy) calculateKeywordScore(doc documentFields, queryTokens []string) float32 {
	if len(queryTokens) == 0 {
		return 0
	}

	var score float32
	query := strings.Join(queryTokens, " ")

	if s.containsLower(doc.AllLower, query) {
		score += s.config.ExactMatchBoost
	}

	if s.containsLower(doc.TitleLower, query) {
		score += s.config.TitleBoost
	}

	matchCount := 0
	for _, token := range queryTokens {
		if doc.TokenSet[token] {
			matchCount++
		}
	}

	if matchCount > 0 {
		ratio := float32(matchCount) / float32(len(queryTokens))
		score += s.config.FuzzyBoost * ratio
	}

	return score
}

func (s *Strategy) calculateCoverage(doc documentFields, queryTokens []string) float32 {
	if len(queryTokens) == 0 || doc.Length == 0 {
		return 0
	}

	matched := 0
	for _, token := range queryTokens {
		if doc.TokenSet[token] {
			matched++
		}
	}

	return float32(matched) / float32(len(queryTokens))
}

func (s *Strategy) calculateRecency(doc documentFields) float32 {
	if s.config.RecencyWeight == 0 || s.config.RecencyField == "" {
		return 0
	}

	ts := doc.Metadata[s.config.RecencyField]
	if ts == "" {
		return 0
	}

	t, err := parseTimestamp(ts)
	if err != nil {
		return 0
	}

	daysSince := time.Since(t).Hours() / 24
	if daysSince > float64(s.config.RecencyDays) {
		return 0
	}

	halflife := s.config.HalfLifeDays
	if halflife <= 0 {
		halflife = 7
	}
	decay := math.Exp(-0.693 * daysSince / halflife)
	return float32(decay)
}

func (s *Strategy) calculateMetadataScore(doc documentFields) float32 {
	var score float32

	for key, boost := range s.config.MetadataBoosts {
		if v, ok := doc.Metadata[key]; ok && (v == "true" || v == "1") {
			score += boost
		}
	}

	for key, penalty := range s.config.MetadataPenalties {
		if v, ok := doc.Metadata[key]; ok && (v == "true" || v == "1") {
			score -= penalty
		}
	}

	return score
}

func (s *Strategy) hasExactMatch(doc documentFields, tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	return doc.TokenSet[tokens[0]]
}

func (s *Strategy) hasTitleMatch(doc documentFields, tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	query := strings.Join(tokens, " ")
	return s.containsLower(doc.TitleLower, query)
}

func (s *Strategy) countMatches(doc documentFields, tokens []string) int {
	count := 0
	for _, t := range tokens {
		if doc.TokenSet[t] {
			count++
		}
	}
	return count
}

func (s *Strategy) containsLower(text, substr string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(substr))
}

func (s *Strategy) explainScore(c candidateScore) string {
	parts := []string{}
	if c.HasExact {
		parts = append(parts, "exact match")
	}
	if c.HasTitle {
		parts = append(parts, "title match")
	}
	if c.MatchCount > 0 {
		parts = append(parts, fmt.Sprintf("+%d tokens", c.MatchCount))
	}
	if len(parts) == 0 {
		return "keyword relevance"
	}
	return strings.Join(parts, ", ")
}

func clamp(value, min, max float32) float32 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func parseTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty")
	}

	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}

	if parsed > 1e12 {
		return time.UnixMilli(parsed), nil
	}
	return time.Unix(parsed, 0), nil
}