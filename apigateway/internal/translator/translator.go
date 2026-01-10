// This file contains translator functions that convert data structures between the
// public-facing API (apigatewaypb) and the internal worker service API (workerpb).
// This decouples the public API from the internal implementation, allowing them to evolve independently.

package translator

import (
	apigatewaypb "github.com/pavandhadge/vectron/shared/proto/apigateway"
	workerpb "github.com/pavandhadge/vectron/shared/proto/worker"
)

// ToWorkerStoreVectorRequestFromPoint translates a public API Point to a worker's StoreVectorRequest.
func ToWorkerStoreVectorRequestFromPoint(point *apigatewaypb.Point, shardID uint64) *workerpb.StoreVectorRequest {
	// TODO: Handle the translation of the payload map[string]string to a byte slice.
	// This could be done by serializing the map to JSON.
	return &workerpb.StoreVectorRequest{
		ShardId: shardID,
		Vector: &workerpb.Vector{
			Id:       point.Id,
			Vector:   point.Vector,
			Metadata: nil, // Placeholder for payload translation.
		},
	}
}

// ToWorkerSearchRequest translates a public SearchRequest to a worker's SearchRequest.
func ToWorkerSearchRequest(req *apigatewaypb.SearchRequest, shardID uint64) *workerpb.SearchRequest {
	return &workerpb.SearchRequest{
		ShardId: shardID,
		Vector:  req.Vector,
		K:       int32(req.TopK),
	}
}

// FromWorkerSearchResponse translates a worker's SearchResponse to the public API's format.
func FromWorkerSearchResponse(res *workerpb.SearchResponse) *apigatewaypb.SearchResponse {
	results := make([]*apigatewaypb.SearchResult, len(res.Ids))
	for i, id := range res.Ids {
		results[i] = &apigatewaypb.SearchResult{
			Id: id,
			// Note: The worker's SearchResponse in the current proto definition does not include
			// scores or payloads. This is a potential area for enhancement in the worker service.
			Score:   res.Scores[i],
			Payload: nil,
		}
	}
	return &apigatewaypb.SearchResponse{
		Results: results,
	}
}

// ToWorkerGetVectorRequest translates a public GetRequest to a worker's GetVectorRequest.
func ToWorkerGetVectorRequest(req *apigatewaypb.GetRequest, shardID uint64) *workerpb.GetVectorRequest {
	return &workerpb.GetVectorRequest{
		ShardId: shardID,
		Id:      req.Id,
	}
}

// FromWorkerGetVectorResponse translates a worker's GetVectorResponse to the public GetResponse format.
func FromWorkerGetVectorResponse(res *workerpb.GetVectorResponse) *apigatewaypb.GetResponse {
	if res.Vector == nil {
		// Return an empty response if the vector was not found.
		return &apigatewaypb.GetResponse{}
	}
	// TODO: Handle the translation of the metadata byte slice back to a map[string]string.
	// This would involve deserializing from JSON if that's the chosen format.
	return &apigatewaypb.GetResponse{
		Point: &apigatewaypb.Point{
			Id:      res.Vector.Id,
			Vector:  res.Vector.Vector,
			Payload: nil, // Placeholder for metadata translation.
		},
	}
}

// ToWorkerDeleteVectorRequest translates a public DeleteRequest to a worker's DeleteVectorRequest.
func ToWorkerDeleteVectorRequest(req *apigatewaypb.DeleteRequest, shardID uint64) *workerpb.DeleteVectorRequest {
	return &workerpb.DeleteVectorRequest{
		ShardId: shardID,
		Id:      req.Id,
	}
}

// FromWorkerDeleteVectorResponse translates a worker's DeleteVectorResponse to the public DeleteResponse.
// The current implementation is a no-op as the response is empty.
func FromWorkerDeleteVectorResponse(res *workerpb.DeleteVectorResponse) *apigatewaypb.DeleteResponse {
	return &apigatewaypb.DeleteResponse{}
}
