-- Feedback database schema
-- This schema stores user feedback for search results to improve reranking

-- Main feedback sessions table
CREATE TABLE IF NOT EXISTS feedback_sessions (
    id TEXT PRIMARY KEY, -- UUID for the feedback session
    collection TEXT NOT NULL,
    query TEXT, -- Original search query (optional)
    user_id TEXT, -- From context
    session_id TEXT, -- From context
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    additional_context TEXT -- JSON string for other context data
);

-- Individual feedback items for each result
CREATE TABLE IF NOT EXISTS feedback_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL, -- Foreign key to feedback_sessions.id
    result_id TEXT NOT NULL, -- ID of the search result
    relevance_score INTEGER NOT NULL CHECK(relevance_score >= 1 AND relevance_score <= 5), -- 1-5 scale
    clicked BOOLEAN DEFAULT FALSE, -- Whether user clicked on this result
    position INTEGER NOT NULL, -- Position in the returned results (0-based)
    comment TEXT, -- Optional user comment
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES feedback_sessions(id)
);

-- Index for efficient queries
CREATE INDEX IF NOT EXISTS idx_feedback_sessions_collection ON feedback_sessions(collection);
CREATE INDEX IF NOT EXISTS idx_feedback_sessions_created_at ON feedback_sessions(created_at);
CREATE INDEX IF NOT EXISTS idx_feedback_items_session_id ON feedback_items(session_id);
CREATE INDEX IF NOT EXISTS idx_feedback_items_result_id ON feedback_items(result_id);
CREATE INDEX IF NOT EXISTS idx_feedback_items_relevance_score ON feedback_items(relevance_score);