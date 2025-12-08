package vectron

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	// Assuming the generated protobuf code is in this package
	"github.com/pavandhadge/vectron/apigateway/proto/apigateway"
)

// Point represents a single vector point.
type Point struct {
	ID      string
	Vector  []float32
	Payload map[string]string
}

// SearchResult represents a single search result.
type SearchResult struct {
	ID      string
	Score   float32
	Payload map[string]string
}

// Client is the Go client for the Vectron vector database.
type Client struct {
	conn   *grpc.ClientConn
	client apigateway.VectronServiceClient
	apiKey string
}

// NewClient creates a new Vectron client.
// It establishes a gRPC connection to the specified host.
func NewClient(host string, apiKey string) (*Client, error) {
	if host == "" {
		return nil, fmt.Errorf("%w: host cannot be empty", ErrInvalidArgument)
	}
	conn, err := grpc.Dial(host, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to apigateway: %w", err)
	}

	return &Client{
		conn:   conn,
		client: apigateway.NewVectronServiceClient(conn),
		apiKey: apiKey,
	}, nil
}

// handleError converts a gRPC error into a more specific client error.
func handleError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.Unauthenticated:
		return fmt.Errorf("%w: %s", ErrAuthentication, st.Message())
	case codes.NotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, st.Message())
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %s", ErrInvalidArgument, st.Message())
	case codes.AlreadyExists:
		return fmt.Errorf("%w: %s", ErrAlreadyExists, st.Message())
	case codes.Internal:
		return fmt.Errorf("%w: %s", ErrInternalServer, st.Message())
	default:
		return err
	}
}

// getContext creates a new context with a timeout and authentication metadata.
func (c *Client) getContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if c.apiKey != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.apiKey)
	}
	return ctx, cancel
}

// CreateCollection creates a new collection in Vectron.
func (c *Client) CreateCollection(name string, dimension int32, distance string) error {
	if name == "" {
		return fmt.Errorf("%w: collection name cannot be empty", ErrInvalidArgument)
	}
	if dimension <= 0 {
		return fmt.Errorf("%w: dimension must be a positive integer", ErrInvalidArgument)
	}

	ctx, cancel := c.getContext()
	defer cancel()

	req := &apigateway.CreateCollectionRequest{
		Name:      name,
		Dimension: dimension,
		Distance:  distance,
	}

	_, err := c.client.CreateCollection(ctx, req)
	return handleError(err)
}

// ListCollections lists all collection names in Vectron.
func (c *Client) ListCollections() ([]string, error) {
	ctx, cancel := c.getContext()
	defer cancel()

	req := &apigateway.ListCollectionsRequest{}
	res, err := c.client.ListCollections(ctx, req)
	if err != nil {
		return nil, handleError(err)
	}
	return res.Collections, nil
}

// Upsert inserts or updates vectors in a collection.
func (c *Client) Upsert(collection string, points []*Point) (int32, error) {
	if collection == "" {
		return 0, fmt.Errorf("%w: collection name cannot be empty", ErrInvalidArgument)
	}
	if len(points) == 0 {
		return 0, nil
	}

	ctx, cancel := c.getContext()
	defer cancel()

	protoPoints := make([]*apigateway.Point, len(points))
	for i, p := range points {
		if p.ID == "" {
			return 0, fmt.Errorf("%w: point ID cannot be empty", ErrInvalidArgument)
		}
		protoPoints[i] = &apigateway.Point{
			Id:      p.ID,
			Vector:  p.Vector,
			Payload: p.Payload,
		}
	}

	req := &apigateway.UpsertRequest{
		Collection: collection,
		Points:     protoPoints,
	}

	res, err := c.client.Upsert(ctx, req)
	if err != nil {
		return 0, handleError(err)
	}
	return res.Upserted, nil
}

// Search finds the k-nearest neighbors to a query vector.
func (c *Client) Search(collection string, vector []float32, topK uint32) ([]*SearchResult, error) {
	if collection == "" {
		return nil, fmt.Errorf("%w: collection name cannot be empty", ErrInvalidArgument)
	}

	ctx, cancel := c.getContext()
	defer cancel()

	req := &apigateway.SearchRequest{
		Collection: collection,
		Vector:     vector,
		TopK:       topK,
	}

	res, err := c.client.Search(ctx, req)
	if err != nil {
		return nil, handleError(err)
	}

	results := make([]*SearchResult, len(res.Results))
	for i, r := range res.Results {
		results[i] = &SearchResult{
			ID:      r.Id,
			Score:   r.Score,
			Payload: r.Payload,
		}
	}
	return results, nil
}

// Get retrieves a point by its ID.
func (c *Client) Get(collection string, pointID string) (*Point, error) {
	if collection == "" || pointID == "" {
		return nil, fmt.Errorf("%w: collection name and point ID cannot be empty", ErrInvalidArgument)
	}

	ctx, cancel := c.getContext()
	defer cancel()

	req := &apigateway.GetRequest{
		Collection: collection,
		Id:         pointID,
	}

	res, err := c.client.Get(ctx, req)
	if err != nil {
		return nil, handleError(err)
	}

	return &Point{
		ID:      res.Point.Id,
		Vector:  res.Point.Vector,
		Payload: res.Point.Payload,
	}, nil
}

// Delete deletes a point by its ID.
func (c *Client) Delete(collection string, pointID string) error {
	if collection == "" || pointID == "" {
		return nil, fmt.Errorf("%w: collection name and point ID cannot be empty", ErrInvalidArgument)
	}

	ctx, cancel := c.getContext()
	defer cancel()

	req := &apigateway.DeleteRequest{
		Collection: collection,
		Id:         pointID,
	}

	_, err := c.client.Delete(ctx, req)
	return handleError(err)
}

// Close closes the underlying gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
