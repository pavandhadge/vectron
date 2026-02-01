# Feedback System Integration

The Vectron system now includes a comprehensive feedback collection system that allows users to provide relevance ratings for search results. This feedback can be used to improve the reranker rules and overall search quality.

## Architecture

```
Client Request (with feedback)
      ↓
  API Gateway (/v1/feedback)
      ↓
  Feedback Service (SQLite)
      ↓
  Analytics & Rule Improvement
```

## API Endpoint

### Submit Feedback

**HTTP**: `POST /v1/feedback`
**gRPC**: `SubmitFeedback`

#### Request Format:

```json
{
  "collection": "my_documents",
  "query": "machine learning tutorial",
  "result_ids": ["doc1", "doc2", "doc3"],
  "feedback_items": [
    {
      "result_id": "doc1",
      "relevance_score": 5,
      "clicked": true,
      "position": 0,
      "comment": "Exactly what I was looking for"
    },
    {
      "result_id": "doc2",
      "relevance_score": 3,
      "clicked": false,
      "position": 1,
      "comment": "Somewhat relevant but not detailed enough"
    }
  ],
  "context": {
    "user_id": "user123",
    "session_id": "sess_456",
    "search_type": "semantic"
  }
}
```

#### Response Format:

```json
{
  "success": true,
  "feedback_id": "fb_789"
}
```

## Feedback Schema

The system uses SQLite with proper separation of concerns:

### Tables

1. **feedback_sessions**: Stores overall feedback session metadata
   - `id`: Unique session identifier (UUID)
   - `collection`: Collection name
   - `query`: Original search query
   - `user_id`: User identifier
   - `session_id`: Session identifier
   - `created_at`: Timestamp
   - `additional_context`: JSON string for extra context

2. **feedback_items**: Stores individual result ratings
   - `session_id`: References feedback_sessions.id
   - `result_id`: Document/result identifier
   - `relevance_score`: 1-5 rating scale
   - `clicked`: Boolean for click tracking
   - `position`: Result position (0-based)
   - `comment`: Optional user comment

## Configuration

Set the feedback database path via environment variable:

```bash
export FEEDBACK_DB_PATH=./data/feedback.db
```

Default: `./data/feedback.db`

## Usage Examples

### Submit Feedback via HTTP

```bash
curl -X POST http://localhost:8080/v1/feedback \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "collection": "documents",
    "query": "neural networks",
    "result_ids": ["doc1", "doc2"],
    "feedback_items": [
      {
        "result_id": "doc1",
        "relevance_score": 5,
        "clicked": true,
        "position": 0
      },
      {
        "result_id": "doc2", 
        "relevance_score": 2,
        "clicked": false,
        "position": 1
      }
    ],
    "context": {
      "user_id": "user123"
    }
  }'
```

### Submit Feedback via gRPC (Go)

```go
client := pb.NewVectronServiceClient(conn)

req := &pb.SubmitFeedbackRequest{
    Collection: "documents",
    Query:      "neural networks",
    ResultIds:  []string{"doc1", "doc2"},
    FeedbackItems: []*pb.FeedbackItem{
        {
            ResultId:       "doc1",
            RelevanceScore: 5,
            Clicked:        true,
            Position:       0,
        },
        {
            ResultId:       "doc2",
            RelevanceScore: 2,
            Clicked:        false,
            Position:       1,
        },
    },
    Context: map[string]string{
        "user_id": "user123",
    },
}

resp, err := client.SubmitFeedback(ctx, req)
```

## Analytics & Insights

The feedback service provides statistics for rule improvement:

```go
stats, err := feedbackService.GetRelevanceStats(ctx, "collection_name")
// Returns:
// - AverageRelevance: Mean relevance score
// - ClickThroughRate: Percentage of clicked results
// - TotalFeedback: Total feedback items
// - TotalSessions: Total feedback sessions
```

## Data for Rule Training

The collected feedback can be used to:

1. **Identify Poorly Ranked Results**: Find results with high similarity scores but low relevance ratings
2. **Improve Keyword Matching**: Analyze queries vs highly-rated results for better rules
3. **Optimize Position Bias**: Understand how position affects click-through rates
4. **User Behavior Analysis**: Track patterns in user preferences
5. **A/B Testing**: Compare different reranking strategies

## Integration Benefits

- **Continuous Improvement**: System learns from real user feedback
- **Quality Metrics**: Objective measures of search relevance
- **User Engagement**: Better results lead to higher satisfaction
- **Rule Optimization**: Data-driven improvements to reranking algorithms
- **Personalization**: Foundation for user-specific ranking preferences

## Security & Privacy

- User IDs are stored as provided (can be hashed/anonymized by client)
- No sensitive content is logged
- SQLite database can be encrypted at rest
- Feedback data can be aggregated and anonymized for analysis