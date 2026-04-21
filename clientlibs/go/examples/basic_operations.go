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
	const (
		apiGatewayAddr = "localhost:10010"
		sdkJWTToken    = "your-sdk-jwt-token"
		collectionName = "basic-demo-collection"
		vectorDim    = 128
	)

	printHeader()

	client, err := vectron.NewClient(apiGatewayAddr, sdkJWTToken)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	printSuccess("Client initialized")

	fmt.Println("\n" + stepStyle("2") + " Creating collection...")
	if err := client.CreateCollection(collectionName, int32(vectorDim), "cosine"); err != nil {
		log.Printf("Collection may already exist: %v", err)
	} else {
		printSuccess(fmt.Sprintf("Collection created (dimension=%d)", vectorDim))
	}
	time.Sleep(2 * time.Second)

	fmt.Println("\n" + stepStyle("3") + " Upserting vectors...")
	points := []*vectron.Point{
		{
			ID:     "product-001",
			Vector: generateEmbedding("electronics"),
			Payload: map[string]string{
				"name":     "Wireless Headphones",
				"category": "electronics",
				"price":    "99.99",
			},
		},
		{
			ID:     "product-002",
			Vector: generateEmbedding("electronics"),
			Payload: map[string]string{
				"name":     "Bluetooth Speaker",
				"category": "electronics",
				"price":    "79.99",
			},
		},
		{
			ID:     "product-003",
			Vector: generateEmbedding("sports"),
			Payload: map[string]string{
				"name":     "Running Shoes",
				"category": "sports",
				"price":    "129.99",
			},
		},
		{
			ID:     "product-004",
			Vector: generateEmbedding("sports"),
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
	printSuccess(fmt.Sprintf("Upserted %d vectors (128-dim)", upserted))

	fmt.Println("\n" + stepStyle("4") + " Listing collections...")
	collections, err := client.ListCollections()
	if err != nil {
		log.Fatalf("Failed to list collections: %v", err)
	}
	fmt.Printf("  Collections: %v\n", collections)

	fmt.Println("\n" + stepStyle("5") + " Searching for electronics...")
	results, err := client.Search(collectionName, generateQuery("electronics"), 10)
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}

	fmt.Printf("\n  \033[1mAll Results (electronics query):\033[0m\n")
	for i, r := range results {
		fmt.Printf("  %d. \033[1;37m%s\033[0m  \033[90m%.1f%%\033[0m  %s\n",
			i+1, r.Payload["name"], r.Score*100, r.Payload["category"])
	}

	fmt.Println("\n" + stepStyle("6") + " Retrieving vector by ID...")
	pt, err := client.Get(collectionName, "product-001")
	if err != nil {
		log.Fatalf("Failed to get point: %v", err)
	}
	fmt.Printf("  ID: %s  dim=%d\n", pt.ID, len(pt.Vector))

	fmt.Println("\n" + stepStyle("7") + " Deleting vector...")
	if err := client.Delete(collectionName, "product-004"); err != nil {
		log.Fatalf("Failed to delete point: %v", err)
	}
	printSuccess("Deleted product-004")

	fmt.Println("\n" + stepStyle("8") + " Collection status...")
	status, err := client.GetCollectionStatus(collectionName)
	if err != nil {
		log.Fatalf("Failed to get collection status: %v", err)
	}
	fmt.Printf("  Name: %s  Dimension: %d  Shards: %d\n",
		status.Name, status.Dimension, len(status.Shards))

	printFooter()
}

var categoryBase = map[string][]float32{
	"electronics": {15.0, 2.0, 0.5, 0.5, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
	"sports":     {2.0, 15.0, 0.5, 0.5, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
}

func generateEmbedding(category string) []float32 {
	base := categoryBase[category]
	vec := make([]float32, 128)
	for i := 0; i < len(base) && i < 128; i++ {
		vec[i] = base[i]
	}
	return vec
}

func generateQuery(category string) []float32 {
	base := categoryBase[category]
	vec := make([]float32, 128)
	for i := 0; i < len(base) && i < 128; i++ {
		vec[i] = base[i]
	}
	return vec
}

func printHeader() {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║          Vectron Basic Operations Demo                      ║")
	fmt.Println("║          128-Dimensional Vector Search                      ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Println()
}

func stepStyle(n string) string {
	return "\033[1;36m[" + n + "]\033[0m"
}

func printSuccess(msg string) {
	fmt.Printf("  \033[1;32m✓\033[0m %s\n", msg)
}

func printFooter() {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║  ✓ Collection created with 128-dim embeddings                     ║")
	fmt.Println("║  ✓ Semantic search across product categories                       ║")
	fmt.Println("║  ✓ Full CRUD operations demonstrated                                 ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════════╝")
}
