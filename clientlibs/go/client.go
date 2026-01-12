// This file implements the Go client for the Vectron vector database.
// It provides a user-friendly interface for interacting with the Vectron API,
// handling gRPC connection, authentication, and error translation.

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

	// This assumes the generated protobuf code is in this package path.
	apigateway "github.com/pavandhadge/vectron/clientlibs/go/proto/apigateway"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
)

// Point represents a single vector point for upserting.
type Point struct {
	ID      string
	Vector  []float32
	Payload map[string]string
}

// SearchResult represents a single result from a search query.
type SearchResult struct {
	ID      string
	Score   float32
	Payload map[string]string
}

// Client is the primary entrypoint for interacting with the Vectron API.
type Client struct {
	conn       *grpc.ClientConn
	client     apigateway.VectronServiceClient
	jwtToken   string
}

// NewClient creates a new Vectron client.
// It establishes a gRPC connection to the specified host address of the API gateway.
func NewClient(host string, jwtToken string) (*Client, error) {
	if host == "" {
		return nil, fmt.Errorf("%w: host cannot be empty", ErrInvalidArgument)
	}
	// Establishes a connection without TLS. For production, use secure credentials.
	conn, err := grpc.Dial(host, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to apigateway: %w", err)
	}

	return &Client{
		conn:       conn,
		client:     apigateway.NewVectronServiceClient(conn),
		jwtToken:   jwtToken,
	}, nil
}

// handleError translates a gRPC status error into a more specific client-side error.
func handleError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		// Not a gRPC status error, return as is.
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
		return err // Return the original gRPC error for unhandled codes.
	}
}

// getContext creates a new context with a default timeout and injects the JWT for authentication.
func (c *Client) getContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if c.jwtToken != "" {
		// Adds "authorization: Bearer <jwtToken>" to the outgoing gRPC metadata.
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.jwtToken)
	}
	return ctx, cancel
}

// CreateCollection sends an RPC to create a new collection in Vectron.
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

// ListCollections retrieves a list of all collection names in Vectron.
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

// GetCollectionStatus retrieves the status of a collection, including the readiness of its shards.
func (c *Client) GetCollectionStatus(collectionName string) (*apigateway.GetCollectionStatusResponse, error) {
	if collectionName == "" {
		return nil, fmt.Errorf("%w: collection name cannot be empty", ErrInvalidArgument)
	}

	ctx, cancel := c.getContext()
	defer cancel()

	req := &apigateway.GetCollectionStatusRequest{
		Name: collectionName,
	}

	res, err := c.client.GetCollectionStatus(ctx, req)
	if err != nil {
		return nil, handleError(err)
	}
	return res, nil
}

// Upsert inserts or updates one or more vectors in a specified collection.
func (c *Client) Upsert(collection string, points []*Point) (int32, error) {
	if collection == "" {
		return 0, fmt.Errorf("%w: collection name cannot be empty", ErrInvalidArgument)
	}
	if len(points) == 0 {
		return 0, nil // Nothing to do.
	}

	ctx, cancel := c.getContext()
	defer cancel()

	// Translate client-side Point structs to the protobuf-generated type.
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

// Search finds the k-nearest neighbors to a query vector in a collection.
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

	// Translate protobuf-generated SearchResult to the client-side type.
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

// Get retrieves a single point by its ID from a collection.
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

	// Translate the protobuf response to the client-side Point type.
	return &Point{
		ID:      res.Point.Id,
		Vector:  res.Point.Vector,
		Payload: res.Point.Payload,
	}, nil
}

// Delete removes a point by its ID from a collection.
func (c *Client) Delete(collection string, pointID string) error {
	if collection == "" || pointID == "" {
		return fmt.Errorf("%w: collection name and point ID cannot be empty", ErrInvalidArgument)
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

// UpdateUserProfile updates the user's profile information.
func (c *Client) UpdateUserProfile(plan authpb.Plan) (*authpb.UserProfile, error) {
	ctx, cancel := c.getContext()
	defer cancel()

	req := &apigateway.UpdateUserProfileRequest{
		Plan: plan,
	}

	res, err := c.client.UpdateUserProfile(ctx, req)
	if err != nil {
		return nil, handleError(err)
	}
	return res.User, nil
}

// Close terminates the underlying gRPC connection to the server.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
