// Package feedback provides functionality for storing and managing user feedback
// on search results to improve reranking algorithms.
package feedback

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// FeedbackItem represents a single feedback item for a search result
type FeedbackItem struct {
	ResultID       string `json:"result_id"`
	RelevanceScore int32  `json:"relevance_score"`
	Clicked        bool   `json:"clicked"`
	Position       int32  `json:"position"`
	Comment        string `json:"comment"`
}

// FeedbackSession represents a complete feedback session
type FeedbackSession struct {
	ID           string                 `json:"id"`
	Collection   string                 `json:"collection"`
	Query        string                 `json:"query"`
	UserID       string                 `json:"user_id"`
	SessionID    string                 `json:"session_id"`
	Items        []FeedbackItem         `json:"items"`
	Context      map[string]string      `json:"context"`
	CreatedAt    time.Time              `json:"created_at"`
}

// Service handles feedback storage and retrieval
type Service struct {
	db *sql.DB
}

// NewService creates a new feedback service with SQLite backend
func NewService(dbPath string) (*Service, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	service := &Service{db: db}
	
	// Initialize database schema
	if err := service.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return service, nil
}

// initSchema creates the database tables if they don't exist
func (s *Service) initSchema() error {
	// Read and execute schema
	schema := `
		-- Main feedback sessions table
		CREATE TABLE IF NOT EXISTS feedback_sessions (
			id TEXT PRIMARY KEY,
			collection TEXT NOT NULL,
			query TEXT,
			user_id TEXT,
			session_id TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			additional_context TEXT
		);

		-- Individual feedback items for each result
		CREATE TABLE IF NOT EXISTS feedback_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			result_id TEXT NOT NULL,
			relevance_score INTEGER NOT NULL CHECK(relevance_score >= 1 AND relevance_score <= 5),
			clicked BOOLEAN DEFAULT FALSE,
			position INTEGER NOT NULL,
			comment TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (session_id) REFERENCES feedback_sessions(id)
		);

		-- Indexes for efficient queries
		CREATE INDEX IF NOT EXISTS idx_feedback_sessions_collection ON feedback_sessions(collection);
		CREATE INDEX IF NOT EXISTS idx_feedback_sessions_created_at ON feedback_sessions(created_at);
		CREATE INDEX IF NOT EXISTS idx_feedback_items_session_id ON feedback_items(session_id);
		CREATE INDEX IF NOT EXISTS idx_feedback_items_result_id ON feedback_items(result_id);
		CREATE INDEX IF NOT EXISTS idx_feedback_items_relevance_score ON feedback_items(relevance_score);
	`
	
	_, err := s.db.Exec(schema)
	return err
}

// StoreFeedback stores a feedback session in the database
func (s *Service) StoreFeedback(ctx context.Context, session *FeedbackSession) (string, error) {
	// Generate session ID if not provided
	if session.ID == "" {
		session.ID = uuid.New().String()
	}

	// Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Serialize additional context to JSON
	contextJSON := ""
	if len(session.Context) > 0 {
		contextBytes, err := json.Marshal(session.Context)
		if err != nil {
			return "", fmt.Errorf("failed to marshal context: %w", err)
		}
		contextJSON = string(contextBytes)
	}

	// Insert feedback session
	_, err = tx.ExecContext(ctx, `
		INSERT INTO feedback_sessions (id, collection, query, user_id, session_id, additional_context)
		VALUES (?, ?, ?, ?, ?, ?)
	`, session.ID, session.Collection, session.Query, session.UserID, session.SessionID, contextJSON)
	if err != nil {
		return "", fmt.Errorf("failed to insert feedback session: %w", err)
	}

	// Insert feedback items
	for _, item := range session.Items {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO feedback_items (session_id, result_id, relevance_score, clicked, position, comment)
			VALUES (?, ?, ?, ?, ?, ?)
		`, session.ID, item.ResultID, item.RelevanceScore, item.Clicked, item.Position, item.Comment)
		if err != nil {
			return "", fmt.Errorf("failed to insert feedback item: %w", err)
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}

	return session.ID, nil
}

// GetFeedbackByCollection retrieves feedback for a specific collection
func (s *Service) GetFeedbackByCollection(ctx context.Context, collection string, limit int) ([]FeedbackSession, error) {
	if limit <= 0 {
		limit = 100 // Default limit
	}

	// Query feedback sessions
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, collection, query, user_id, session_id, created_at, additional_context
		FROM feedback_sessions
		WHERE collection = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, collection, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query feedback sessions: %w", err)
	}
	defer rows.Close()

	var sessions []FeedbackSession
	for rows.Next() {
		var session FeedbackSession
		var contextJSON sql.NullString
		
		err = rows.Scan(&session.ID, &session.Collection, &session.Query, 
			&session.UserID, &session.SessionID, &session.CreatedAt, &contextJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan feedback session: %w", err)
		}

		// Parse context JSON
		if contextJSON.Valid {
			json.Unmarshal([]byte(contextJSON.String), &session.Context)
		}

		// Load feedback items for this session
		items, err := s.getFeedbackItems(ctx, session.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to load feedback items: %w", err)
		}
		session.Items = items

		sessions = append(sessions, session)
	}

	return sessions, nil
}

// getFeedbackItems retrieves feedback items for a specific session
func (s *Service) getFeedbackItems(ctx context.Context, sessionID string) ([]FeedbackItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT result_id, relevance_score, clicked, position, comment
		FROM feedback_items
		WHERE session_id = ?
		ORDER BY position
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []FeedbackItem
	for rows.Next() {
		var item FeedbackItem
		err = rows.Scan(&item.ResultID, &item.RelevanceScore, &item.Clicked, &item.Position, &item.Comment)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, nil
}

// Close closes the database connection
func (s *Service) Close() error {
	return s.db.Close()
}

// GetRelevanceStats gets relevance statistics for improving rules
func (s *Service) GetRelevanceStats(ctx context.Context, collection string) (*RelevanceStats, error) {
	query := `
		SELECT 
			AVG(CAST(fi.relevance_score as FLOAT)) as avg_relevance,
			COUNT(*) as total_feedback,
			SUM(CASE WHEN fi.clicked THEN 1 ELSE 0 END) as total_clicks,
			COUNT(DISTINCT fs.id) as total_sessions
		FROM feedback_items fi
		JOIN feedback_sessions fs ON fi.session_id = fs.id
		WHERE fs.collection = ?
	`
	
	var stats RelevanceStats
	var totalClicks int
	
	err := s.db.QueryRowContext(ctx, query, collection).Scan(
		&stats.AverageRelevance, &stats.TotalFeedback, &totalClicks, &stats.TotalSessions,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get relevance stats: %w", err)
	}
	
	if stats.TotalFeedback > 0 {
		stats.ClickThroughRate = float64(totalClicks) / float64(stats.TotalFeedback)
	}
	
	return &stats, nil
}

// RelevanceStats provides statistics about user feedback
type RelevanceStats struct {
	AverageRelevance  float64 `json:"average_relevance"`
	ClickThroughRate  float64 `json:"click_through_rate"`
	TotalFeedback     int     `json:"total_feedback"`
	TotalSessions     int     `json:"total_sessions"`
}