# API Gateway + Reranker Integration

The API Gateway now integrates with the Reranker service to provide enhanced search results that go beyond simple vector similarity.

## Architecture

```
Client Request
      ↓
  API Gateway
      ↓
  Worker Service (vector search)
      ↓
  Reranker Service (rule-based enhancement)
      ↓
  Enhanced Results
      ↓
     Client
```

## Configuration

Set the reranker service address via environment variable:

```bash
export RERANKER_SERVICE_ADDR=localhost:50051
```

## Complete Flow

1. **Client sends search request** to API Gateway
2. **API Gateway** forwards request to Worker Service with increased `top_k` (2x requested amount)
3. **Worker Service** returns vector similarity results
4. **API Gateway** sends results to Reranker Service with:
   - Candidate IDs and similarity scores
   - Metadata from search results
5. **Reranker Service** applies rule-based reranking:
   - Boosts exact matches
   - Applies title relevance
   - Considers metadata signals
6. **API Gateway** returns reranked results to client

## Features

- **Automatic Integration**: All search requests are automatically enhanced
- **Graceful Degradation**: If reranker fails, returns original vector results
- **Performance Optimization**: Requests more results from worker for better reranking
- **Flexible Configuration**: Easy to configure reranker address
- **Error Handling**: Robust error handling with fallback to vector results

## Usage

Start all services:

```bash
# 1. Start Placement Driver
cd placementdriver && ./bin/placementdriver

# 2. Start Worker Service  
cd worker && ./bin/worker

# 3. Start Reranker Service
cd reranker && ./bin/reranker --strategy=rule --port=50051

# 4. Start API Gateway
cd apigateway && RERANKER_SERVICE_ADDR=localhost:50051 ./bin/apigateway
```

Now all search requests will automatically benefit from intelligent reranking!