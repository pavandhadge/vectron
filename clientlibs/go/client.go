// This file implements the Go client for the Vectron vector database.
// It provides a user-friendly interface for interacting with the Vectron API,
// handling gRPC connection, authentication, and error translation.

package vectron

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	// Use shared proto definitions to avoid conflicts
	apigateway "github.com/pavandhadge/vectron/shared/proto/apigateway"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
)

const (
	defaultTimeout       = 10 * time.Second
	defaultDialTimeout   = 10 * time.Second
	defaultMaxMsgSize    = 16 * 1024 * 1024
	defaultKeepaliveTime = 30 * time.Second
	defaultKeepaliveTO   = 10 * time.Second
	defaultUserAgent     = "vectron-go-client"
	defaultBatchSize     = 256
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// ClientOptions configures connection and safety/performance settings.
type ClientOptions struct {
	// Timeout is the per-RPC timeout. Use a negative value to disable timeouts.
	Timeout time.Duration
	// DialTimeout is the timeout for establishing the gRPC connection.
	DialTimeout time.Duration
	// MaxRecvMsgSize sets the max inbound gRPC message size in bytes.
	MaxRecvMsgSize int
	// MaxSendMsgSize sets the max outbound gRPC message size in bytes.
	MaxSendMsgSize int
	// KeepaliveTime controls client-side keepalive ping interval.
	KeepaliveTime time.Duration
	// KeepaliveTimeout controls how long to wait for keepalive ack.
	KeepaliveTimeout time.Duration
	// KeepalivePermitWithoutStream allows pings with no active streams.
	KeepalivePermitWithoutStream bool
	// UseTLS enables TLS. If false, an insecure channel is used.
	UseTLS bool
	// TLSConfig provides custom TLS configuration when UseTLS is true.
	TLSConfig *tls.Config
	// TLSServerName sets the TLS server name override (SNI).
	TLSServerName string
	// ExpectedVectorDim validates vectors against a fixed dimension when > 0.
	ExpectedVectorDim int
	// UserAgent sets a custom gRPC user agent string.
	UserAgent string
	// RetryPolicy controls automatic retries for transient failures.
	RetryPolicy RetryPolicy
	// Compression sets gRPC compression (e.g., "gzip"). Empty disables.
	Compression string
	// HedgedReads enables duplicate read requests after a delay to cut tail latency.
	HedgedReads bool
	// HedgeDelay controls when the hedged read is issued.
	HedgeDelay time.Duration
}

// RetryPolicy defines retry behavior for transient failures.
type RetryPolicy struct {
	// MaxAttempts is the maximum attempts including the initial call.
	MaxAttempts int
	// InitialBackoff is the base backoff duration for retries.
	InitialBackoff time.Duration
	// MaxBackoff is the maximum backoff duration.
	MaxBackoff time.Duration
	// BackoffMultiplier controls exponential backoff growth.
	BackoffMultiplier float64
	// Jitter applies random jitter to backoff in the range [0, Jitter].
	Jitter float64
	// RetryOnWrites enables retries for non-idempotent operations.
	RetryOnWrites bool
	// RetryOnCodes defines gRPC codes that are retryable.
	RetryOnCodes []codes.Code
}

// DefaultClientOptions returns the safe, performance-oriented defaults.
func DefaultClientOptions() ClientOptions {
	return ClientOptions{
		Timeout:          defaultTimeout,
		DialTimeout:      defaultDialTimeout,
		MaxRecvMsgSize:   defaultMaxMsgSize,
		MaxSendMsgSize:   defaultMaxMsgSize,
		KeepaliveTime:    defaultKeepaliveTime,
		KeepaliveTimeout: defaultKeepaliveTO,
		UserAgent:        defaultUserAgent,
		Compression:      "",
		HedgedReads:      false,
		HedgeDelay:       75 * time.Millisecond,
		RetryPolicy: RetryPolicy{
			MaxAttempts:       3,
			InitialBackoff:    100 * time.Millisecond,
			MaxBackoff:        2 * time.Second,
			BackoffMultiplier: 2,
			Jitter:            0.2,
			RetryOnWrites:     false,
			RetryOnCodes: []codes.Code{
				codes.Unavailable,
				codes.DeadlineExceeded,
				codes.ResourceExhausted,
			},
		},
	}
}

func normalizeOptions(opts *ClientOptions) ClientOptions {
	base := DefaultClientOptions()
	if opts == nil {
		return base
	}
	if opts.Timeout != 0 {
		base.Timeout = opts.Timeout
	}
	if opts.DialTimeout != 0 {
		base.DialTimeout = opts.DialTimeout
	}
	if opts.MaxRecvMsgSize != 0 {
		base.MaxRecvMsgSize = opts.MaxRecvMsgSize
	}
	if opts.MaxSendMsgSize != 0 {
		base.MaxSendMsgSize = opts.MaxSendMsgSize
	}
	if opts.KeepaliveTime != 0 {
		base.KeepaliveTime = opts.KeepaliveTime
	}
	if opts.KeepaliveTimeout != 0 {
		base.KeepaliveTimeout = opts.KeepaliveTimeout
	}
	if opts.UserAgent != "" {
		base.UserAgent = opts.UserAgent
	}
	if opts.Compression != "" {
		base.Compression = opts.Compression
	}
	if opts.HedgeDelay != 0 {
		base.HedgeDelay = opts.HedgeDelay
	}
	base.HedgedReads = opts.HedgedReads
	base.KeepalivePermitWithoutStream = opts.KeepalivePermitWithoutStream
	base.UseTLS = opts.UseTLS
	base.TLSConfig = opts.TLSConfig
	if opts.TLSServerName != "" {
		base.TLSServerName = opts.TLSServerName
	}
	base.ExpectedVectorDim = opts.ExpectedVectorDim
	if opts.RetryPolicy.MaxAttempts != 0 ||
		opts.RetryPolicy.InitialBackoff != 0 ||
		opts.RetryPolicy.MaxBackoff != 0 ||
		opts.RetryPolicy.BackoffMultiplier != 0 ||
		opts.RetryPolicy.Jitter != 0 ||
		opts.RetryPolicy.RetryOnWrites ||
		len(opts.RetryPolicy.RetryOnCodes) > 0 {
		base.RetryPolicy = opts.RetryPolicy
	}
	return base
}

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
	conn     *grpc.ClientConn
	client   apigateway.VectronServiceClient
	jwtToken string
	opts     ClientOptions
}

// BatchOptions controls client-side batching behavior.
type BatchOptions struct {
	// BatchSize is the max points per batch.
	BatchSize int
	// MaxBatchBytes caps batch payload size in bytes (approximate).
	MaxBatchBytes int
	// Concurrency is number of concurrent batch requests.
	Concurrency int
}

// NewClient creates a new Vectron client.
// It establishes a gRPC connection to the specified host address of the API gateway.
func NewClient(host string, jwtToken string) (*Client, error) {
	return NewClientWithOptions(host, jwtToken, nil)
}

// NewClientWithOptions creates a new Vectron client with advanced options.
func NewClientWithOptions(host string, jwtToken string, opts *ClientOptions) (*Client, error) {
	if host == "" {
		return nil, fmt.Errorf("%w: host cannot be empty", ErrInvalidArgument)
	}

	options := normalizeOptions(opts)
	var creds credentials.TransportCredentials
	if options.UseTLS {
		if options.TLSConfig != nil {
			creds = credentials.NewTLS(options.TLSConfig)
		} else {
			tlsConfig := &tls.Config{}
			if options.TLSServerName != "" {
				tlsConfig.ServerName = options.TLSServerName
			}
			creds = credentials.NewTLS(tlsConfig)
		}
	} else {
		creds = insecure.NewCredentials()
	}

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
	}
	callOpts := []grpc.CallOption{}
	if options.MaxRecvMsgSize > 0 {
		callOpts = append(callOpts, grpc.MaxCallRecvMsgSize(options.MaxRecvMsgSize))
	}
	if options.MaxSendMsgSize > 0 {
		callOpts = append(callOpts, grpc.MaxCallSendMsgSize(options.MaxSendMsgSize))
	}
	if len(callOpts) > 0 {
		dialOpts = append(dialOpts, grpc.WithDefaultCallOptions(callOpts...))
	}
	if options.KeepaliveTime > 0 || options.KeepaliveTimeout > 0 {
		dialOpts = append(dialOpts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                options.KeepaliveTime,
			Timeout:             options.KeepaliveTimeout,
			PermitWithoutStream: options.KeepalivePermitWithoutStream,
		}))
	}
	if options.UserAgent != "" {
		dialOpts = append(dialOpts, grpc.WithUserAgent(options.UserAgent))
	}
	if options.Compression != "" {
		dialOpts = append(dialOpts, grpc.WithDefaultCallOptions(grpc.UseCompressor(options.Compression)))
	}

	ctx := context.Background()
	if options.DialTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), options.DialTimeout)
		defer cancel()
	}

	conn, err := grpc.DialContext(ctx, host, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to apigateway: %w", err)
	}

	return &Client{
		conn:     conn,
		client:   apigateway.NewVectronServiceClient(conn),
		jwtToken: jwtToken,
		opts:     options,
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
	ctx := context.Background()
	var cancel context.CancelFunc
	if c.opts.Timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), c.opts.Timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	if c.jwtToken != "" {
		// Adds "authorization: Bearer <jwtToken>" to the outgoing gRPC metadata.
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.jwtToken)
	}
	return ctx, cancel
}

func (c *Client) validateVectorLength(vector []float32) error {
	if c.opts.ExpectedVectorDim <= 0 {
		return nil
	}
	if len(vector) != c.opts.ExpectedVectorDim {
		return fmt.Errorf("%w: vector dimension %d does not match expected %d", ErrInvalidArgument, len(vector), c.opts.ExpectedVectorDim)
	}
	return nil
}

func (c *Client) doRPC(isWrite bool, fn func(context.Context) error) error {
	policy := c.opts.RetryPolicy
	maxAttempts := policy.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ctx, cancel := c.getContext()
		err := fn(ctx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		shouldRetry, backoff := c.shouldRetry(err, isWrite, attempt)
		if !shouldRetry {
			return err
		}
		time.Sleep(backoff)
	}
	return lastErr
}

func (c *Client) doRPCWithResponse(isWrite bool, fn func(context.Context) (interface{}, error)) (interface{}, error) {
	var zero interface{}
	policy := c.opts.RetryPolicy
	maxAttempts := policy.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := c.doHedgedCall(isWrite, fn)
		if err == nil {
			return res, nil
		}
		lastErr = err
		shouldRetry, backoff := c.shouldRetry(err, isWrite, attempt)
		if !shouldRetry {
			return zero, err
		}
		time.Sleep(backoff)
	}
	return zero, lastErr
}

func (c *Client) doHedgedCall(isWrite bool, fn func(context.Context) (interface{}, error)) (interface{}, error) {
	if isWrite || !c.opts.HedgedReads {
		ctx, cancel := c.getContext()
		res, err := fn(ctx)
		cancel()
		return res, err
	}

	hedgeDelay := c.opts.HedgeDelay
	if hedgeDelay <= 0 {
		hedgeDelay = 50 * time.Millisecond
	}

	type result struct {
		res interface{}
		err error
	}
	results := make(chan result, 2)

	ctx1, cancel1 := c.getContext()
	go func() {
		res, err := fn(ctx1)
		results <- result{res: res, err: err}
	}()

	timer := time.NewTimer(hedgeDelay)
	defer timer.Stop()

	select {
	case r := <-results:
		cancel1()
		return r.res, r.err
	case <-timer.C:
	}

	ctx2, cancel2 := c.getContext()
	go func() {
		res, err := fn(ctx2)
		results <- result{res: res, err: err}
	}()

	r := <-results
	cancel1()
	cancel2()
	return r.res, r.err
}

func (c *Client) shouldRetry(err error, isWrite bool, attempt int) (bool, time.Duration) {
	policy := c.opts.RetryPolicy
	if policy.MaxAttempts <= 1 || attempt >= policy.MaxAttempts {
		return false, 0
	}
	if isWrite && !policy.RetryOnWrites {
		return false, 0
	}
	st, ok := status.FromError(err)
	if !ok {
		return false, 0
	}
	code := st.Code()
	retryable := false
	for _, c := range policy.RetryOnCodes {
		if c == code {
			retryable = true
			break
		}
	}
	if !retryable {
		return false, 0
	}

	backoff := policy.InitialBackoff
	if backoff <= 0 {
		backoff = 50 * time.Millisecond
	}
	multiplier := policy.BackoffMultiplier
	if multiplier <= 1 {
		multiplier = 2
	}
	for i := 1; i < attempt; i++ {
		backoff = time.Duration(float64(backoff) * multiplier)
		if policy.MaxBackoff > 0 && backoff > policy.MaxBackoff {
			backoff = policy.MaxBackoff
			break
		}
	}
	if policy.Jitter > 0 {
		jitterFrac := (rand.Float64()*2 - 1) * policy.Jitter
		backoff = time.Duration(float64(backoff) * (1 + jitterFrac))
		if backoff < 0 {
			backoff = 0
		}
	}
	return true, backoff
}

// CreateCollection sends an RPC to create a new collection in Vectron.
func (c *Client) CreateCollection(name string, dimension int32, distance string) error {
	if name == "" {
		return fmt.Errorf("%w: collection name cannot be empty", ErrInvalidArgument)
	}
	if dimension <= 0 {
		return fmt.Errorf("%w: dimension must be a positive integer", ErrInvalidArgument)
	}

	req := &apigateway.CreateCollectionRequest{
		Name:      name,
		Dimension: dimension,
		Distance:  distance,
	}

	err := c.doRPC(true, func(ctx context.Context) error {
		_, err := c.client.CreateCollection(ctx, req)
		return err
	})
	return handleError(err)
}

// ListCollections retrieves a list of all collection names in Vectron.
func (c *Client) ListCollections() ([]string, error) {
	req := &apigateway.ListCollectionsRequest{}
	resRaw, err := c.doRPCWithResponse(false, func(ctx context.Context) (interface{}, error) {
		return c.client.ListCollections(ctx, req)
	})
	if err != nil {
		return nil, handleError(err)
	}
	res, ok := resRaw.(*apigateway.ListCollectionsResponse)
	if !ok {
		return nil, fmt.Errorf("%w: invalid response type", ErrInternalServer)
	}
	return res.Collections, nil
}

// GetCollectionStatus retrieves the status of a collection, including the readiness of its shards.
func (c *Client) GetCollectionStatus(collectionName string) (*apigateway.GetCollectionStatusResponse, error) {
	if collectionName == "" {
		return nil, fmt.Errorf("%w: collection name cannot be empty", ErrInvalidArgument)
	}

	req := &apigateway.GetCollectionStatusRequest{
		Name: collectionName,
	}

	resRaw, err := c.doRPCWithResponse(false, func(ctx context.Context) (interface{}, error) {
		return c.client.GetCollectionStatus(ctx, req)
	})
	if err != nil {
		return nil, handleError(err)
	}
	res, ok := resRaw.(*apigateway.GetCollectionStatusResponse)
	if !ok {
		return nil, fmt.Errorf("%w: invalid response type", ErrInternalServer)
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

	// Translate client-side Point structs to the protobuf-generated type.
	protoPoints := make([]*apigateway.Point, len(points))
	for i, p := range points {
		if p.ID == "" {
			return 0, fmt.Errorf("%w: point ID cannot be empty", ErrInvalidArgument)
		}
		if len(p.Vector) == 0 {
			return 0, fmt.Errorf("%w: point vector cannot be empty", ErrInvalidArgument)
		}
		if err := c.validateVectorLength(p.Vector); err != nil {
			return 0, err
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

	resRaw, err := c.doRPCWithResponse(true, func(ctx context.Context) (interface{}, error) {
		return c.client.Upsert(ctx, req)
	})
	if err != nil {
		return 0, handleError(err)
	}
	res, ok := resRaw.(*apigateway.UpsertResponse)
	if !ok {
		return 0, fmt.Errorf("%w: invalid response type", ErrInternalServer)
	}
	return res.Upserted, nil
}

// Search finds the k-nearest neighbors to a query vector in a collection.
func (c *Client) Search(collection string, vector []float32, topK uint32) ([]*SearchResult, error) {
	if collection == "" {
		return nil, fmt.Errorf("%w: collection name cannot be empty", ErrInvalidArgument)
	}
	if len(vector) == 0 {
		return nil, fmt.Errorf("%w: query vector cannot be empty", ErrInvalidArgument)
	}
	if err := c.validateVectorLength(vector); err != nil {
		return nil, err
	}

	req := &apigateway.SearchRequest{
		Collection: collection,
		Vector:     vector,
		TopK:       topK,
	}

	resRaw, err := c.doRPCWithResponse(false, func(ctx context.Context) (interface{}, error) {
		return c.client.Search(ctx, req)
	})
	if err != nil {
		return nil, handleError(err)
	}
	res, ok := resRaw.(*apigateway.SearchResponse)
	if !ok {
		return nil, fmt.Errorf("%w: invalid response type", ErrInternalServer)
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

	req := &apigateway.GetRequest{
		Collection: collection,
		Id:         pointID,
	}

	resRaw, err := c.doRPCWithResponse(false, func(ctx context.Context) (interface{}, error) {
		return c.client.Get(ctx, req)
	})
	if err != nil {
		return nil, handleError(err)
	}
	res, ok := resRaw.(*apigateway.GetResponse)
	if !ok {
		return nil, fmt.Errorf("%w: invalid response type", ErrInternalServer)
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

	req := &apigateway.DeleteRequest{
		Collection: collection,
		Id:         pointID,
	}

	err := c.doRPC(true, func(ctx context.Context) error {
		_, err := c.client.Delete(ctx, req)
		return err
	})
	return handleError(err)
}

// UpdateUserProfile updates the user's profile information.
func (c *Client) UpdateUserProfile(plan authpb.Plan) (*authpb.UserProfile, error) {
	req := &apigateway.UpdateUserProfileRequest{
		Plan: plan,
	}

	resRaw, err := c.doRPCWithResponse(true, func(ctx context.Context) (interface{}, error) {
		return c.client.UpdateUserProfile(ctx, req)
	})
	if err != nil {
		return nil, handleError(err)
	}
	res, ok := resRaw.(*apigateway.UpdateUserProfileResponse)
	if !ok {
		return nil, fmt.Errorf("%w: invalid response type", ErrInternalServer)
	}
	return res.User, nil
}

func estimatePointBytes(p *Point) int {
	if p == nil {
		return 0
	}
	size := len(p.ID) + len(p.Vector)*4
	for k, v := range p.Payload {
		size += len(k) + len(v)
	}
	return size + 16
}

// UpsertBatch splits large upserts into batches for higher throughput.
func (c *Client) UpsertBatch(collection string, points []*Point, opts *BatchOptions) (int32, error) {
	if collection == "" {
		return 0, fmt.Errorf("%w: collection name cannot be empty", ErrInvalidArgument)
	}
	if len(points) == 0 {
		return 0, nil
	}
	batchSize := defaultBatchSize
	maxBatchBytes := 0
	if opts != nil && opts.BatchSize > 0 {
		batchSize = opts.BatchSize
	}
	if opts != nil && opts.MaxBatchBytes > 0 {
		maxBatchBytes = opts.MaxBatchBytes
	}
	concurrency := 1
	if opts != nil && opts.Concurrency > 0 {
		concurrency = opts.Concurrency
	}

	batches := make(chan []*Point, concurrency)
	var total int32
	var firstErr error
	var errMu sync.Mutex
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for batch := range batches {
			if firstErr != nil {
				return
			}
			upserted, err := c.Upsert(collection, batch)
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return
			}
			atomic.AddInt32(&total, upserted)
		}
	}

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go worker()
	}

	if maxBatchBytes <= 0 {
		for i := 0; i < len(points); i += batchSize {
			end := i + batchSize
			if end > len(points) {
				end = len(points)
			}
			if firstErr != nil {
				break
			}
			batches <- points[i:end]
		}
	} else {
		start := 0
		for start < len(points) {
			if firstErr != nil {
				break
			}
			curBytes := 0
			end := start
			for end < len(points) && end-start < batchSize {
				pointBytes := estimatePointBytes(points[end])
				if end == start || curBytes+pointBytes <= maxBatchBytes {
					curBytes += pointBytes
					end++
					continue
				}
				break
			}
			batches <- points[start:end]
			start = end
		}
	}
	close(batches)
	wg.Wait()

	if firstErr != nil {
		return 0, firstErr
	}
	return total, nil
}

// ClientPool provides a simple round-robin pool of clients for high-QPS use.
type ClientPool struct {
	clients []*Client
	idx     uint32
}

// NewClientPool creates a pool of clients.
func NewClientPool(host string, jwtToken string, opts *ClientOptions, size int) (*ClientPool, error) {
	if size <= 0 {
		size = 1
	}
	clients := make([]*Client, 0, size)
	for i := 0; i < size; i++ {
		client, err := NewClientWithOptions(host, jwtToken, opts)
		if err != nil {
			for _, c := range clients {
				_ = c.Close()
			}
			return nil, err
		}
		clients = append(clients, client)
	}
	return &ClientPool{clients: clients}, nil
}

// Next returns the next client in round-robin order.
func (p *ClientPool) Next() *Client {
	if len(p.clients) == 0 {
		return nil
	}
	i := atomic.AddUint32(&p.idx, 1)
	return p.clients[int(i-1)%len(p.clients)]
}

// Close closes all pooled clients.
func (p *ClientPool) Close() error {
	var firstErr error
	for _, c := range p.clients {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Close terminates the underlying gRPC connection to the server.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
