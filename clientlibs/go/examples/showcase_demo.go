// showcase_demo.go - Demonstrates a broader set of Vectron client capabilities
//
// This example shows how to:
// - Configure client options for safety/performance
// - Create a collection and wait until it is ready
// - Upsert a batch of vectors with payloads
// - Run a similarity search
// - Retrieve a vector by ID
// - Delete a vector
//
// Run: go run showcase_demo.go

package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	vectron "github.com/pavandhadge/vectron/clientlibs/go"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func main() {
	const (
		apiGatewayAddr = "localhost:10010"
		authGRPCAddr   = "localhost:10008"
		collectionName = "testy"
	)

	rand.Seed(time.Now().UnixNano())
	email := fmt.Sprintf("demo-%d@example.com", rand.Intn(1_000_000))
	password := "DemoPassword123!"
	if v := os.Getenv("DEMO_EMAIL"); v != "" {
		email = v
	}
	if v := os.Getenv("DEMO_PASSWORD"); v != "" {
		password = v
	}

	jwtToken, err := registerAndLogin(authGRPCAddr, email, password)
	if err != nil {
		log.Fatalf("auth failed: %v", err)
	}

	apiKeyName := fmt.Sprintf("demo-key-%d", rand.Intn(1_000_000))
	fullKey, keyPrefix, err := createAPIKey(authGRPCAddr, jwtToken, apiKeyName)
	if err != nil {
		log.Fatalf("create api key failed: %v", err)
	}
	log.Printf("Created API key prefix=%s", keyPrefix)

	sdkJWTToken, err := createSDKJWT(authGRPCAddr, jwtToken, keyPrefix)
	if err != nil {
		log.Fatalf("create sdk jwt failed: %v", err)
	}
	log.Printf("SDK JWT created from key %s (full key=%s)", keyPrefix, fullKey)

	// Configure client options for production-friendly defaults.
	opts := vectron.DefaultClientOptions()
	opts.Timeout = 8 * time.Second
	opts.ExpectedVectorDim = 4
	opts.Compression = "gzip"
	opts.HedgedReads = true
	opts.HedgeDelay = 50 * time.Millisecond

	client, err := vectron.NewClientWithOptions(apiGatewayAddr, sdkJWTToken, &opts)
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	// Create collection
	if err := client.CreateCollection(collectionName, 4, "euclidean"); err != nil {
		log.Printf("collection may already exist: %v", err)
	}

	// Wait for the collection to become ready
	deadline := time.Now().Add(20 * time.Second)
	for {
		status, err := client.GetCollectionStatus(collectionName)
		if err == nil && len(status.Shards) > 0 {
			allReady := true
			for _, shard := range status.Shards {
				if !shard.Ready {
					allReady = false
					break
				}
			}
			if allReady {
				break
			}
		}
		if time.Now().After(deadline) {
			log.Fatalf("collection %q did not become ready in time", collectionName)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Upsert vectors with payloads
	points := []*vectron.Point{
		{
			ID:     "doc-001",
			Vector: []float32{0.1, 0.2, 0.3, 0.4},
			Payload: map[string]string{
				"title":    "Vector Databases 101",
				"category": "docs",
			},
		},
		{
			ID:     "doc-002",
			Vector: []float32{0.11, 0.21, 0.31, 0.41},
			Payload: map[string]string{
				"title":    "Approximate Nearest Neighbor",
				"category": "docs",
			},
		},
		{
			ID:     "doc-003",
			Vector: []float32{0.8, 0.7, 0.6, 0.5},
			Payload: map[string]string{
				"title":    "Production Indexing",
				"category": "ops",
			},
		},
	}

	upserted, err := client.Upsert(collectionName, points)
	if err != nil {
		log.Fatalf("upsert failed: %v", err)
	}
	fmt.Printf("Upserted %d points\n", upserted)

	// Search
	results, err := client.Search(collectionName, []float32{0.12, 0.22, 0.32, 0.42}, 2)
	if err != nil {
		log.Fatalf("search failed: %v", err)
	}
	fmt.Println("Top results:")
	for _, r := range results {
		fmt.Printf("  id=%s score=%.4f payload=%v\n", r.ID, r.Score, r.Payload)
	}

	// Get a point by ID
	point, err := client.Get(collectionName, "doc-001")
	if err != nil {
		log.Fatalf("get failed: %v", err)
	}
	fmt.Printf("Fetched point: id=%s payload=%v\n", point.ID, point.Payload)

	// Delete a point
	if err := client.Delete(collectionName, "doc-003"); err != nil {
		log.Fatalf("delete failed: %v", err)
	}
	fmt.Println("Deleted point doc-003")
}

func registerAndLogin(authAddr, email, password string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, authAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	client := authpb.NewAuthServiceClient(conn)

	_, err = client.RegisterUser(ctx, &authpb.RegisterUserRequest{
		Email:    email,
		Password: password,
	})
	if err != nil {
		log.Printf("register warning: %v", err)
	}

	loginResp, err := client.Login(ctx, &authpb.LoginRequest{
		Email:    email,
		Password: password,
	})
	if err != nil {
		return "", err
	}
	if loginResp.GetJwtToken() == "" {
		return "", fmt.Errorf("empty jwt token after login")
	}
	return loginResp.GetJwtToken(), nil
}

func createAPIKey(authAddr, jwtToken, name string) (fullKey, keyPrefix string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, authAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", "", err
	}
	defer conn.Close()
	client := authpb.NewAuthServiceClient(conn)

	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+jwtToken))
	resp, err := client.CreateAPIKey(authCtx, &authpb.CreateAPIKeyRequest{Name: name})
	if err != nil {
		return "", "", err
	}
	if resp.GetFullKey() == "" || resp.GetKeyInfo().GetKeyPrefix() == "" {
		return "", "", fmt.Errorf("missing key info in response")
	}
	return resp.GetFullKey(), resp.GetKeyInfo().GetKeyPrefix(), nil
}

func createSDKJWT(authAddr, jwtToken, keyPrefix string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, authAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	client := authpb.NewAuthServiceClient(conn)

	authCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+jwtToken))
	resp, err := client.CreateSDKJWT(authCtx, &authpb.CreateSDKJWTRequest{ApiKeyId: keyPrefix})
	if err != nil {
		return "", err
	}
	if resp.GetSdkJwt() == "" {
		return "", fmt.Errorf("empty sdk jwt")
	}
	return resp.GetSdkJwt(), nil
}
