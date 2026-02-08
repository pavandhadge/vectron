// This file contains translator functions that convert data structures between the
// public-facing API (apigatewaypb) and the internal worker service API (workerpb).
// This decouples the public API from the internal implementation, allowing them to evolve independently.

package translator

import (
	"encoding/json"
	"log"

	apigatewaypb "github.com/pavandhadge/vectron/shared/proto/apigateway"
	workerpb "github.com/pavandhadge/vectron/shared/proto/worker"
)

func encodePayload(payload map[string]string) []byte {
	if len(payload) == 0 {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("translator: failed to encode payload: %v", err)
		return nil
	}
	return data
}

func decodeMetadata(metadata []byte) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]string)
	if err := json.Unmarshal(metadata, &out); err != nil {
		log.Printf("translator: failed to decode metadata: %v", err)
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ToWorkerStoreVectorRequestFromPoint translates a public API Point to a worker's StoreVectorRequest.
func ToWorkerStoreVectorRequestFromPoint(point *apigatewaypb.Point, shardID uint64, shardEpoch uint64, leaseExpiryUnixMs int64) *workerpb.StoreVectorRequest {
	return &workerpb.StoreVectorRequest{
		ShardId:           shardID,
		ShardEpoch:        shardEpoch,
		LeaseExpiryUnixMs: leaseExpiryUnixMs,
		Vector: &workerpb.Vector{
			Id:       point.Id,
			Vector:   point.Vector,
			Metadata: encodePayload(point.Payload),
		},
	}
}

// ToWorkerVectorFromPoint translates a public API Point to a worker Vector.
func ToWorkerVectorFromPoint(point *apigatewaypb.Point) *workerpb.Vector {
	return &workerpb.Vector{
		Id:       point.Id,
		Vector:   point.Vector,
		Metadata: encodePayload(point.Payload),
	}
}

// ToWorkerBatchStoreVectorRequestFromPoints translates points to a batch store request.
func ToWorkerBatchStoreVectorRequestFromPoints(points []*apigatewaypb.Point, shardID uint64, shardEpoch uint64, leaseExpiryUnixMs int64) *workerpb.BatchStoreVectorRequest {
	vectors := make([]*workerpb.Vector, 0, len(points))
	for _, point := range points {
		if point == nil {
			continue
		}
		vectors = append(vectors, ToWorkerVectorFromPoint(point))
	}
	return &workerpb.BatchStoreVectorRequest{
		ShardId:           shardID,
		ShardEpoch:        shardEpoch,
		LeaseExpiryUnixMs: leaseExpiryUnixMs,
		Vectors:           vectors,
	}
}

// ToWorkerSearchRequest translates a public SearchRequest to a worker's SearchRequest.
func ToWorkerSearchRequest(req *apigatewaypb.SearchRequest, shardID uint64, shardEpoch uint64, leaseExpiryUnixMs int64, linearizable bool) *workerpb.SearchRequest {
	return &workerpb.SearchRequest{
		ShardId:           shardID,
		ShardEpoch:        shardEpoch,
		LeaseExpiryUnixMs: leaseExpiryUnixMs,
		Vector:            req.Vector,
		K:                 int32(req.TopK),
		Linearizable:      linearizable,
		Collection:        req.Collection,
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
func ToWorkerGetVectorRequest(req *apigatewaypb.GetRequest, shardID uint64, shardEpoch uint64, leaseExpiryUnixMs int64) *workerpb.GetVectorRequest {
	return &workerpb.GetVectorRequest{
		ShardId:           shardID,
		ShardEpoch:        shardEpoch,
		LeaseExpiryUnixMs: leaseExpiryUnixMs,
		Id:                req.Id,
	}
}

// FromWorkerGetVectorResponse translates a worker's GetVectorResponse to the public GetResponse format.
func FromWorkerGetVectorResponse(res *workerpb.GetVectorResponse) *apigatewaypb.GetResponse {
	if res.Vector == nil {
		// Return an empty response if the vector was not found.
		return &apigatewaypb.GetResponse{}
	}
	return &apigatewaypb.GetResponse{
		Point: &apigatewaypb.Point{
			Id:      res.Vector.Id,
			Vector:  res.Vector.Vector,
			Payload: decodeMetadata(res.Vector.Metadata),
		},
	}
}

// ToWorkerDeleteVectorRequest translates a public DeleteRequest to a worker's DeleteVectorRequest.
func ToWorkerDeleteVectorRequest(req *apigatewaypb.DeleteRequest, shardID uint64, shardEpoch uint64, leaseExpiryUnixMs int64) *workerpb.DeleteVectorRequest {
	return &workerpb.DeleteVectorRequest{
		ShardId:           shardID,
		ShardEpoch:        shardEpoch,
		LeaseExpiryUnixMs: leaseExpiryUnixMs,
		Id:                req.Id,
	}
}

// FromWorkerDeleteVectorResponse translates a worker's DeleteVectorResponse to the public DeleteResponse.
// The current implementation is a no-op as the response is empty.
func FromWorkerDeleteVectorResponse(res *workerpb.DeleteVectorResponse) *apigatewaypb.DeleteResponse {
	return &apigatewaypb.DeleteResponse{}
}
