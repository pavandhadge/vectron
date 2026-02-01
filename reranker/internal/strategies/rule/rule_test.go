package rule

import (
	"context"
	"testing"
	"time"

	"github.com/pavandhadge/vectron/reranker/internal"
)

func TestRuleBasedStrategy_Name(t *testing.T) {
	strategy := NewStrategy(DefaultConfig())
	if name := strategy.Name(); name != "rule-based" {
		t.Errorf("expected name 'rule-based', got '%s'", name)
	}
}

func TestRuleBasedStrategy_BasicReranking(t *testing.T) {
	config := DefaultConfig()
	strategy := NewStrategy(config)
	
	input := &internal.RerankInput{
		Query: "machine learning tutorial",
		Candidates: []internal.Candidate{
			{
				ID:    "doc1",
				Score: 0.7,
				Metadata: map[string]string{
					"title":   "Introduction to Machine Learning",
					"content": "This is a comprehensive tutorial about machine learning basics.",
				},
			},
			{
				ID:    "doc2",
				Score: 0.8,
				Metadata: map[string]string{
					"title":   "Advanced Neural Networks",
					"content": "Deep dive into neural network architectures.",
				},
			},
			{
				ID:    "doc3",
				Score: 0.6,
				Metadata: map[string]string{
					"title":   "Machine Learning Tutorial for Beginners",
					"content": "Step-by-step machine learning tutorial with examples.",
				},
			},
		},
		TopN: 3,
	}
	
	output, err := strategy.Rerank(context.Background(), input)
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}
	
	if len(output.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(output.Results))
	}
	
	// doc3 should rank highest due to exact title match and content relevance
	if output.Results[0].ID != "doc3" {
		t.Logf("Results order: %v", []string{output.Results[0].ID, output.Results[1].ID, output.Results[2].ID})
		t.Logf("Scores: doc1=%.3f, doc2=%.3f, doc3=%.3f",
			getScoreByID(output.Results, "doc1"),
			getScoreByID(output.Results, "doc2"),
			getScoreByID(output.Results, "doc3"))
	}
	
	// All scores should be in 0-1 range
	for _, result := range output.Results {
		if result.RerankScore < 0 || result.RerankScore > 1 {
			t.Errorf("score out of range [0,1]: %.3f", result.RerankScore)
		}
	}
}

func TestRuleBasedStrategy_ExactMatchBoost(t *testing.T) {
	config := DefaultConfig()
	config.ExactMatchBoost = 0.5
	strategy := NewStrategy(config)
	
	input := &internal.RerankInput{
		Query: "python programming",
		Candidates: []internal.Candidate{
			{
				ID:    "doc1",
				Score: 0.5,
				Metadata: map[string]string{
					"content": "Learn python programming from scratch.",
				},
			},
			{
				ID:    "doc2",
				Score: 0.9,
				Metadata: map[string]string{
					"content": "JavaScript is a popular language.",
				},
			},
		},
		TopN: 2,
	}
	
	output, err := strategy.Rerank(context.Background(), input)
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}
	
	// doc1 should rank higher despite lower original score due to exact match
	if output.Results[0].ID != "doc1" {
		t.Errorf("expected doc1 to rank first, got %s (scores: doc1=%.3f, doc2=%.3f)",
			output.Results[0].ID,
			getScoreByID(output.Results, "doc1"),
			getScoreByID(output.Results, "doc2"))
	}
}

func TestRuleBasedStrategy_MetadataBoost(t *testing.T) {
	config := DefaultConfig()
	config.MetadataBoosts = map[string]float32{
		"verified": 0.3,
	}
	strategy := NewStrategy(config)
	
	input := &internal.RerankInput{
		Query: "test query",
		Candidates: []internal.Candidate{
			{
				ID:    "doc1",
				Score: 0.5,
				Metadata: map[string]string{
					"content":  "Some content here.",
					"verified": "true",
				},
			},
			{
				ID:    "doc2",
				Score: 0.6,
				Metadata: map[string]string{
					"content": "Other content here.",
				},
			},
		},
		TopN: 2,
	}
	
	output, err := strategy.Rerank(context.Background(), input)
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}
	
	// doc1 should rank higher due to metadata boost
	if output.Results[0].ID != "doc1" {
		t.Errorf("expected doc1 to rank first with metadata boost")
	}
}

func TestRuleBasedStrategy_MetadataPenalty(t *testing.T) {
	config := DefaultConfig()
	config.MetadataPenalties = map[string]float32{
		"deprecated": 0.5,
	}
	strategy := NewStrategy(config)
	
	input := &internal.RerankInput{
		Query: "test query",
		Candidates: []internal.Candidate{
			{
				ID:    "doc1",
				Score: 0.9,
				Metadata: map[string]string{
					"content":    "Great content.",
					"deprecated": "true",
				},
			},
			{
				ID:    "doc2",
				Score: 0.6,
				Metadata: map[string]string{
					"content": "Good content.",
				},
			},
		},
		TopN: 2,
	}
	
	output, err := strategy.Rerank(context.Background(), input)
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}
	
	// doc2 might rank higher due to doc1's penalty
	doc1Score := getScoreByID(output.Results, "doc1")
	doc2Score := getScoreByID(output.Results, "doc2")
	
	if doc1Score >= 0.9 {
		t.Errorf("expected doc1 score to be penalized below 0.9, got %.3f", doc1Score)
	}
	
	t.Logf("Scores after penalty: doc1=%.3f, doc2=%.3f", doc1Score, doc2Score)
}

func TestRuleBasedStrategy_RecencyBoost(t *testing.T) {
	config := DefaultConfig()
	config.RecencyBoost = 0.2
	config.RecencyDays = 30
	config.RecencyField = "created_at"
	strategy := NewStrategy(config)
	
	recentTime := time.Now().Add(-5 * 24 * time.Hour).Format(time.RFC3339)
	oldTime := time.Now().Add(-60 * 24 * time.Hour).Format(time.RFC3339)
	
	input := &internal.RerankInput{
		Query: "test query",
		Candidates: []internal.Candidate{
			{
				ID:    "doc1",
				Score: 0.5,
				Metadata: map[string]string{
					"content":    "Recent document.",
					"created_at": recentTime,
				},
			},
			{
				ID:    "doc2",
				Score: 0.7,
				Metadata: map[string]string{
					"content":    "Old document.",
					"created_at": oldTime,
				},
			},
		},
		TopN: 2,
	}
	
	output, err := strategy.Rerank(context.Background(), input)
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}
	
	doc1Score := getScoreByID(output.Results, "doc1")
	doc2Score := getScoreByID(output.Results, "doc2")
	
	// Recent doc should get a boost (score should increase from original 0.5)
	// With recency boost of 0.2 and ~5 days old (within 30 days), expect some boost
	if doc1Score <= doc2Score {
		t.Errorf("expected recent doc1 to score higher than old doc2, got doc1=%.3f, doc2=%.3f", doc1Score, doc2Score)
	}
	
	t.Logf("Scores with recency: doc1(recent)=%.3f, doc2(old)=%.3f", doc1Score, doc2Score)
}

func TestRuleBasedStrategy_TopNLimiting(t *testing.T) {
	strategy := NewStrategy(DefaultConfig())
	
	input := &internal.RerankInput{
		Query: "test",
		Candidates: []internal.Candidate{
			{ID: "doc1", Score: 0.9, Metadata: map[string]string{"content": "test"}},
			{ID: "doc2", Score: 0.8, Metadata: map[string]string{"content": "test"}},
			{ID: "doc3", Score: 0.7, Metadata: map[string]string{"content": "test"}},
			{ID: "doc4", Score: 0.6, Metadata: map[string]string{"content": "test"}},
			{ID: "doc5", Score: 0.5, Metadata: map[string]string{"content": "test"}},
		},
		TopN: 3,
	}
	
	output, err := strategy.Rerank(context.Background(), input)
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}
	
	if len(output.Results) != 3 {
		t.Errorf("expected 3 results (TopN), got %d", len(output.Results))
	}
}

func TestTokenize(t *testing.T) {
	strategy := NewStrategy(DefaultConfig())
	
	tests := []struct {
		input    string
		expected int // minimum expected tokens
	}{
		{"machine learning tutorial", 3},
		{"The quick brown fox", 3}, // "the" is stop word
		{"Python, JavaScript & Go!", 3},
		{"", 0},
	}
	
	for _, tt := range tests {
		tokens := strategy.tokenize(tt.input)
		if len(tokens) < tt.expected {
			t.Errorf("tokenize(%q) got %d tokens, expected at least %d: %v",
				tt.input, len(tokens), tt.expected, tokens)
		}
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		wantError bool
	}{
		{
			name:      "valid default config",
			config:    DefaultConfig(),
			wantError: false,
		},
		{
			name: "invalid TFIDFWeight",
			config: Config{
				TFIDFWeight:    1.5,
				OriginalWeight: 0.5,
			},
			wantError: true,
		},
		{
			name: "negative RecencyDays",
			config: Config{
				TFIDFWeight:    0.4,
				OriginalWeight: 0.6,
				RecencyDays:    -10,
			},
			wantError: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateConfig() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// Helper function to find score by document ID
func getScoreByID(results []internal.ScoredCandidate, id string) float32 {
	for _, r := range results {
		if r.ID == id {
			return r.RerankScore
		}
	}
	return -1
}
