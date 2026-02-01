package feedback

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"
)

func TestFeedbackService(t *testing.T) {
	// Create unique temporary database
	rand.Seed(time.Now().UnixNano())
	dbPath := fmt.Sprintf("/tmp/feedback_test_%d.db", rand.Intn(100000))
	defer os.Remove(dbPath)
	
	service, err := NewService(dbPath)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	defer service.Close()
	
	ctx := context.Background()
	
	// Test storing feedback
	session := &FeedbackSession{
		Collection: "test_collection",
		Query:      "machine learning tutorial",
		UserID:     "user123",
		Items: []FeedbackItem{
			{ResultID: "doc1", RelevanceScore: 5, Clicked: true, Position: 0},
			{ResultID: "doc2", RelevanceScore: 3, Clicked: false, Position: 1},
		},
		Context: map[string]string{"session_id": "sess_123"},
	}
	
	feedbackID, err := service.StoreFeedback(ctx, session)
	if err != nil {
		t.Fatalf("Failed to store feedback: %v", err)
	}
	
	if feedbackID == "" {
		t.Fatal("Expected non-empty feedback ID")
	}
	
	// Test retrieval
	sessions, err := service.GetFeedbackByCollection(ctx, "test_collection", 10)
	if err != nil {
		t.Fatalf("Failed to retrieve feedback: %v", err)
	}
	
	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}
	
	retrievedSession := sessions[0]
	if retrievedSession.Collection != "test_collection" {
		t.Errorf("Expected collection 'test_collection', got '%s'", retrievedSession.Collection)
	}
	
	if len(retrievedSession.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(retrievedSession.Items))
	}
	
	// Test stats
	stats, err := service.GetRelevanceStats(ctx, "test_collection")
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}
	
	if stats.TotalFeedback != 2 {
		t.Errorf("Expected 2 total feedback, got %d", stats.TotalFeedback)
	}
	
	if stats.TotalSessions != 1 {
		t.Errorf("Expected 1 total sessions, got %d", stats.TotalSessions)
	}
	
	expectedAvg := 4.0 // (5 + 3) / 2
	if stats.AverageRelevance != expectedAvg {
		t.Errorf("Expected average relevance %.1f, got %.1f", expectedAvg, stats.AverageRelevance)
	}
	
	expectedCTR := 0.5 // 1 click out of 2 items
	if stats.ClickThroughRate != expectedCTR {
		t.Errorf("Expected CTR %.1f, got %.1f", expectedCTR, stats.ClickThroughRate)
	}
}