// basic_operations.go - Demonstrates fundamental vector database operations
//
// This example shows how to:
// - Create a collection
// - Upsert vectors with metadata
// - Search for similar vectors
// - Retrieve specific vectors by ID
// - Delete vectors
//
// Run: go run basic_operations.go

package main

import (
	"fmt"
	"log"
	"time"

	vectron "github.com/pavandhadge/vectron/clientlibs/go"
)

func main() {
	// Configuration
	const (
		apiGatewayAddr = "localhost:10010"
		sdkJWTToken    = "your-sdk-jwt-token" // Replace with your actual token
		collectionName = "basic-demo-collection"
	)

	fmt.Println("🚀 Vectron Basic Operations Demo")
	fmt.Println("=================================")

	// Initialize client
	fmt.Println("\n1️⃣  Initializing Vectron client...")
	client, err := vectron.NewClient(apiGatewayAddr, sdkJWTToken)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	fmt.Println("✅ Client initialized successfully")

	// Create collection
	fmt.Println("\n2️⃣  Creating collection...")
	dimension := int32(4) // Small dimension for demo (use 384, 768, or 1536 for real embeddings)
	distance := "euclidean"

	if err := client.CreateCollection(collectionName, dimension, distance); err != nil {
		log.Printf("Collection may already exist: %v", err)
	} else {
		fmt.Printf("✅ Collection '%s' created (dimension=%d, distance=%s)\n", collectionName, dimension, distance)
	}

	// Wait for collection to be ready
	fmt.Println("⏳ Waiting for collection to be ready...")
	time.Sleep(2 * time.Second)

	// Upsert vectors
	fmt.Println("\n3️⃣  Upserting vectors...")
	points := []*vectron.Point{
		{
			ID:     "product-001",
			Vector: []float32{0.1, 0.2, 0.3, 0.4},
			Payload: map[string]string{
				"name":     "Wireless Headphones",
				"category": "electronics",
				"price":    "99.99",
			},
		},
		{
			ID:     "product-002",
			Vector: []float32{0.15, 0.25, 0.35, 0.45},
			Payload: map[string]string{
				"name":     "Bluetooth Speaker",
				"category": "electronics",
				"price":    "79.99",
			},
		},
		{
			ID:     "product-003",
			Vector: []float32{0.8, 0.7, 0.6, 0.5},
			Payload: map[string]string{
				"name":     "Running Shoes",
				"category": "sports",
				"price":    "129.99",
			},
		},
		{
			ID:     "product-004",
			Vector: []float32{0.82, 0.72, 0.62, 0.52},
			Payload: map[string]string{
				"name":     "Yoga Mat",
				"category": "sports",
				"price":    "29.99",
			},
		},
	}

	upserted, err := client.Upsert(collectionName, points)
	if err != nil {
		log.Fatalf("Failed to upsert points: %v", err)
	}
	fmt.Printf("✅ Upserted %d vectors\n", upserted)

	// List collections
	fmt.Println("\n4️⃣  Listing collections...")
	collections, err := client.ListCollections()
	if err != nil {
		log.Fatalf("Failed to list collections: %v", err)
	}
	fmt.Printf("📚 Collections: %v\n", collections)

	// Search for similar vectors
	fmt.Println("\n5️⃣  Searching for similar vectors...")
	queryVector := []float32{0.12, 0.22, 0.32, 0.42} // Similar to electronics products
	topK := uint32(3)

	results, err := client.Search(collectionName, queryVector, topK)
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}

	fmt.Printf("🔍 Search results (top %d):\n", topK)
	for i, result := range results {
		fmt.Printf("  %d. ID: %s, Score: %.4f\n", i+1, result.ID, result.Score)
		if name, ok := result.Payload["name"]; ok {
			fmt.Printf("     Name: %s\n", name)
		}
		if category, ok := result.Payload["category"]; ok {
			fmt.Printf("     Category: %s\n", category)
		}
	}

	// Get specific vector by ID
	fmt.Println("\n6️⃣  Retrieving specific vector...")
	pointID := "product-001"
	point, err := client.Get(collectionName, pointID)
	if err != nil {
		log.Fatalf("Failed to get point: %v", err)
	}
	fmt.Printf("📄 Point '%s':\n", pointID)
	fmt.Printf("   Vector: %v\n", point.Vector)
	fmt.Printf("   Payload: %v\n", point.Payload)

	// Another search - sports products
	fmt.Println("\n7️⃣  Searching for sports products...")
	sportsQuery := []float32{0.81, 0.71, 0.61, 0.51} // Similar to sports products
	sportsResults, err := client.Search(collectionName, sportsQuery, uint32(2))
	if err != nil {
		log.Fatalf("Sports search failed: %v", err)
	}

	fmt.Printf("🔍 Sports search results:\n")
	for i, result := range sportsResults {
		fmt.Printf("  %d. ID: %s, Score: %.4f, Name: %s\n",
			i+1, result.ID, result.Score, result.Payload["name"])
	}

	// Delete a vector
	fmt.Println("\n8️⃣  Deleting a vector...")
	deleteID := "product-004"
	if err := client.Delete(collectionName, deleteID); err != nil {
		log.Fatalf("Failed to delete point: %v", err)
	}
	fmt.Printf("✅ Deleted point '%s'\n", deleteID)

	// Verify deletion
	fmt.Println("\n9️⃣  Verifying deletion...")
	_, err = client.Get(collectionName, deleteID)
	if err != nil {
		fmt.Printf("✅ Point '%s' successfully deleted (not found as expected)\n", deleteID)
	} else {
		fmt.Printf("⚠️  Point '%s' still exists\n", deleteID)
	}

	// Get collection status
	fmt.Println("\n🔟  Getting collection status...")
	status, err := client.GetCollectionStatus(collectionName)
	if err != nil {
		log.Fatalf("Failed to get collection status: %v", err)
	}
	fmt.Printf("📊 Collection Status:\n")
	fmt.Printf("   Name: %s\n", status.Name)
	fmt.Printf("   Dimension: %d\n", status.Dimension)
	fmt.Printf("   Distance: %s\n", status.Distance)
	fmt.Printf("   Shards: %d\n", len(status.Shards))
	for _, shard := range status.Shards {
		fmt.Printf("     Shard %d: Ready=%v, Replicas=%d\n",
			shard.ShardId, shard.Ready, len(shard.Replicas))
	}

	fmt.Println("\n✨ Demo completed successfully!")
	fmt.Println("\nNext steps:")
	fmt.Println("  - Try batch_operations.go for high-throughput scenarios")
	fmt.Println("  - Try ecommerce_search.go for a real-world use case")
	fmt.Println("  - Explore the web console at http://localhost:10011")
}
