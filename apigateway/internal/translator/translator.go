package translator

import (
	apigatewaypb "github.com/pavandhadge/vectron/apigateway/proto/apigateway"
	workerpb "github.com/pavandhadge/vectron/apigateway/proto/worker"
)

// ToWorkerStoreVectorRequestFromPoint translates a public API Point to a worker Vector for storage.
func ToWorkerStoreVectorRequestFromPoint(point *apigatewaypb.Point) *workerpb.StoreVectorRequest {
	return &workerpb.StoreVectorRequest{
		Vector: &workerpb.Vector{
			Id:       point.Id,
			Vector:   point.Vector,
			Metadata: nil, // TODO: Handle metadata/payload from map[string]string to bytes
		},
	}
}

// ToWorkerSearchRequest translates a SearchRequest from the public API to the worker's format.
func ToWorkerSearchRequest(req *apigatewaypb.SearchRequest) *workerpb.SearchRequest {
	return &workerpb.SearchRequest{
		Vector: req.Vector,
		K:      int32(req.TopK),
	}
}

// FromWorkerSearchResponse translates a SearchResponse from the worker to the public API's format.
func FromWorkerSearchResponse(res *workerpb.SearchResponse) *apigatewaypb.SearchResponse {
	results := make([]*apigatewaypb.SearchResult, len(res.Ids))
	for i, id := range res.Ids {
		results[i] = &apigatewaypb.SearchResult{
			Id: id,
			// Note: The worker's SearchResponse does not include score or payload.
			// This would be a point of improvement for the worker service.
		}
	}
	return &apigatewaypb.SearchResponse{
		Results: results,
	}
}

// ToWorkerGetVectorRequest translates a GetRequest to a GetVectorRequest.
func ToWorkerGetVectorRequest(req *apigatewaypb.GetRequest) *workerpb.GetVectorRequest {
	return &workerpb.GetVectorRequest{
		Id: req.Id,
	}
}

// FromWorkerGetVectorResponse translates a GetVectorResponse to a GetResponse.
func FromWorkerGetVectorResponse(res *workerpb.GetVectorResponse) *apigatewaypb.GetResponse {
	if res.Vector == nil {
		return &apigatewaypb.GetResponse{}
	}
	return &apigatewaypb.GetResponse{
		Point: &apigatewaypb.Point{
			Id:      res.Vector.Id,
			Vector:  res.Vector.Vector,
			Payload: nil, // TODO: Handle metadata/payload from bytes to map[string]string
		},
	}
}

// ToWorkerDeleteVectorRequest translates a DeleteRequest to a DeleteVectorRequest.
func ToWorkerDeleteVectorRequest(req *apigatewaypb.DeleteRequest) *workerpb.DeleteVectorRequest {
	return &workerpb.DeleteVectorRequest{
		Id: req.Id,
	}
}

// FromWorkerDeleteVectorResponse translates a DeleteVectorResponse to a DeleteResponse.
func FromWorkerDeleteVectorResponse(res *workerpb.DeleteVectorResponse) *apigatewaypb.DeleteResponse {
	return &apigatewaypb.DeleteResponse{}
}
