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
	"fmt"
	"log"
	"time"

	vectron "github.com/pavandhadge/vectron/clientlibs/go"
)

func main() {
	const (
		apiGatewayAddr = "localhost:10010"
		sdkJWTToken    = "your-sdk-jwt-token" // Replace with your actual token
		collectionName = "showcase-demo-collection"
	)

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
