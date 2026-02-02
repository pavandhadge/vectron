// ecommerce_search.go - Real-world e-commerce semantic search example
//
// This example demonstrates:
// - Indexing a product catalog with vector embeddings
// - Semantic search for products
// - Metadata filtering by category and price
// - Personalized recommendations based on user preferences
//
// Run: go run ecommerce_search.go

package main

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"time"

	vectron "github.com/pavandhadge/vectron/clientlibs/go"
)

// Product represents an e-commerce product
type Product struct {
	ID          string
	Name        string
	Category    string
	Price       float64
	Description string
	Embedding   []float32 // Pre-computed vector embedding
}

// Sample product catalog with embeddings (simulated)
// In production, these would come from an ML model (e.g., OpenAI, HuggingFace)
var products = []Product{
	{
		ID:          "phone-001",
		Name:        "iPhone 15 Pro",
		Category:    "smartphones",
		Price:       999.99,
		Description: "Latest flagship smartphone with advanced camera system",
		Embedding:   []float32{0.9, 0.8, 0.1, 0.2}, // High-tech electronics signature
	},
	{
		ID:          "phone-002",
		Name:        "Samsung Galaxy S24",
		Category:    "smartphones",
		Price:       899.99,
		Description: "Premium Android phone with AI features",
		Embedding:   []float32{0.85, 0.75, 0.15, 0.25},
	},
	{
		ID:          "laptop-001",
		Name:        "MacBook Pro 16",
		Category:    "laptops",
		Price:       2499.99,
		Description: "Professional laptop for developers and creatives",
		Embedding:   []float32{0.8, 0.9, 0.2, 0.1},
	},
	{
		ID:          "laptop-002",
		Name:        "Dell XPS 15",
		Category:    "laptops",
		Price:       1799.99,
		Description: "High-performance Windows laptop",
		Embedding:   []float32{0.75, 0.85, 0.25, 0.15},
	},
	{
		ID:          "headphones-001",
		Name:        "Sony WH-1000XM5",
		Category:    "audio",
		Price:       399.99,
		Description: "Premium noise-canceling wireless headphones",
		Embedding:   []float32{0.7, 0.6, 0.3, 0.4},
	},
	{
		ID:          "headphones-002",
		Name:        "AirPods Pro 2",
		Category:    "audio",
		Price:       249.99,
		Description: "Apple wireless earbuds with spatial audio",
		Embedding:   []float32{0.75, 0.65, 0.2, 0.35},
	},
	{
		ID:          "watch-001",
		Name:        "Apple Watch Ultra",
		Category:    "wearables",
		Price:       799.99,
		Description: "Rugged smartwatch for outdoor activities",
		Embedding:   []float32{0.6, 0.5, 0.7, 0.3},
	},
	{
		ID:          "watch-002",
		Name:        "Garmin Fenix 7",
		Category:    "wearables",
		Price:       699.99,
		Description: "Advanced GPS multisport watch",
		Embedding:   []float32{0.55, 0.45, 0.75, 0.25},
	},
	{
		ID:          "tablet-001",
		Name:        "iPad Pro 12.9",
		Category:    "tablets",
		Price:       1099.99,
		Description: "Professional tablet with M2 chip",
		Embedding:   []float32{0.65, 0.7, 0.3, 0.2},
	},
	{
		ID:          "camera-001",
		Name:        "Sony Alpha 7 IV",
		Category:    "cameras",
		Price:       2499.99,
		Description: "Full-frame mirrorless camera",
		Embedding:   []float32{0.5, 0.4, 0.8, 0.6},
	},
}

func main() {
	const (
		apiGatewayAddr = "localhost:10010"
		sdkJWTToken    = "your-sdk-jwt-token"
		collectionName = "ecommerce-products"
	)

	fmt.Println("🛍️  E-Commerce Semantic Search Demo")
	fmt.Println("====================================")

	// Initialize client
	fmt.Println("\n1️⃣  Connecting to Vectron...")
	client, err := vectron.NewClient(apiGatewayAddr, sdkJWTToken)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	fmt.Println("✅ Connected!")

	// Create collection for products
	fmt.Println("\n2️⃣  Creating product catalog collection...")
	dimension := int32(4)
	if err := client.CreateCollection(collectionName, dimension, "cosine"); err != nil {
		log.Printf("Collection may already exist: %v", err)
	} else {
		fmt.Printf("✅ Collection created for %d products\n", len(products))
	}
	time.Sleep(2 * time.Second)

	// Index products
	fmt.Println("\n3️⃣  Indexing product catalog...")
	points := make([]*vectron.Point, len(products))
	for i, product := range products {
		points[i] = &vectron.Point{
			ID:      product.ID,
			Vector:  product.Embedding,
			Payload: productToPayload(product),
		}
	}

	upserted, err := client.Upsert(collectionName, points)
	if err != nil {
		log.Fatalf("Failed to index products: %v", err)
	}
	fmt.Printf("✅ Indexed %d products\n", upserted)

	// Demo 1: Search for high-end smartphones
	fmt.Println("\n4️⃣  Search: Premium smartphones...")
	// Query vector represents "premium smartphone"
	queryVector := []float32{0.88, 0.78, 0.12, 0.22}
	results, err := client.Search(collectionName, queryVector, 3)
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}

	displayResults("Premium Smartphones", results)

	// Demo 2: Search for professional laptops
	fmt.Println("\n5️⃣  Search: Professional laptops...")
	laptopQuery := []float32{0.78, 0.88, 0.18, 0.12}
	laptopResults, err := client.Search(collectionName, laptopQuery, 3)
	if err != nil {
		log.Fatalf("Laptop search failed: %v", err)
	}
	displayResults("Professional Laptops", laptopResults)

	// Demo 3: Search for wearable devices (filter by category)
	fmt.Println("\n6️⃣  Search: Wearable devices...")
	wearableQuery := []float32{0.58, 0.48, 0.72, 0.28}
	wearableResults, err := client.Search(collectionName, wearableQuery, 5)
	if err != nil {
		log.Fatalf("Wearable search failed: %v", err)
	}

	// Filter results by category
	fmt.Println("🔍 Wearable Devices Results (filtered):")
	for i, result := range wearableResults {
		if result.Payload["category"] == "wearables" {
			displayProduct(i+1, result)
		}
	}

	// Demo 4: Budget-conscious search (under $500)
	fmt.Println("\n7️⃣  Search: Audio devices under $500...")
	audioQuery := []float32{0.72, 0.62, 0.25, 0.38}
	allAudioResults, err := client.Search(collectionName, audioQuery, 10)
	if err != nil {
		log.Fatalf("Audio search failed: %v", err)
	}

	// Filter by price
	fmt.Println("🔍 Audio Devices Under $500:")
	count := 0
	for _, result := range allAudioResults {
		price, _ := strconv.ParseFloat(result.Payload["price"], 64)
		if price < 500 {
			count++
			displayProduct(count, result)
		}
	}

	// Demo 5: Personalized recommendation
	fmt.Println("\n8️⃣  Personalized recommendation based on viewing history...")
	// User viewed iPhone 15 Pro, so recommend similar high-end electronics
	userHistory := []float32{0.88, 0.78, 0.12, 0.22} // iPhone embedding
	recommendations, err := client.Search(collectionName, userHistory, 5)
	if err != nil {
		log.Fatalf("Recommendation failed: %v", err)
	}

	fmt.Println("🎯 Recommended for you (based on iPhone 15 Pro):")
	for i, result := range recommendations {
		if result.ID != "phone-001" { // Exclude the item the user already viewed
			displayProduct(i, result)
		}
	}

	// Demo 6: Similar products (Item-to-item similarity)
	fmt.Println("\n9️⃣  Similar products to AirPods Pro 2...")
	// Get the AirPods embedding and find similar items
	airpodsEmbedding := []float32{0.75, 0.65, 0.2, 0.35}
	similarResults, err := client.Search(collectionName, airpodsEmbedding, 4)
	if err != nil {
		log.Fatalf("Similarity search failed: %v", err)
	}

	fmt.Println("🔍 Similar to AirPods Pro 2:")
	for i, result := range similarResults {
		if result.ID != "headphones-002" { // Exclude the query item
			displayProduct(i, result)
		}
	}

	// Demo 7: Price-based similarity (expensive items)
	fmt.Println("\n🔟  Premium product recommendations (>$1000)...")
	// Get all products and filter by price
	allResults, err := client.Search(collectionName, []float32{0.5, 0.5, 0.5, 0.5}, 20)
	if err != nil {
		log.Fatalf("Premium search failed: %v", err)
	}

	fmt.Println("💎 Premium Products (>$1000):")
	count = 0
	for _, result := range allResults {
		price, _ := strconv.ParseFloat(result.Payload["price"], 64)
		if price > 1000 {
			count++
			displayProduct(count, result)
		}
	}

	fmt.Println("\n✨ E-commerce demo completed!")
	fmt.Println("\nKey Takeaways:")
	fmt.Println("  ✅ Semantic search finds similar products by meaning")
	fmt.Println("  ✅ Metadata filtering enables category/price constraints")
	fmt.Println("  ✅ Vector similarity powers personalized recommendations")
	fmt.Println("  ✅ Item-to-item similarity suggests related products")
}

// productToPayload converts a Product to a map[string]string payload
func productToPayload(p Product) map[string]string {
	return map[string]string{
		"name":        p.Name,
		"category":    p.Category,
		"price":       fmt.Sprintf("%.2f", p.Price),
		"description": p.Description,
	}
}

// displayResults shows search results in a formatted way
func displayResults(query string, results []*vectron.SearchResult) {
	fmt.Printf("🔍 Results for '%s':\n", query)
	for i, result := range results {
		displayProduct(i+1, result)
	}
}

// displayProduct shows a single product with formatting
func displayProduct(rank int, result *vectron.SearchResult) {
	name := result.Payload["name"]
	category := result.Payload["category"]
	price := result.Payload["price"]
	desc := result.Payload["description"]

	fmt.Printf("  %d. %s ($%s) [%.2f]\n", rank, name, price, result.Score)
	fmt.Printf("     Category: %s | ID: %s\n", category, result.ID)
	if len(desc) > 50 {
		desc = desc[:47] + "..."
	}
	fmt.Printf("     %s\n", desc)
}

// calculateSimilarity returns a normalized similarity score (0-100%)
func calculateSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	dotProduct := 0.0
	normA := 0.0
	normB := 0.0

	for i := 0; i < len(a); i++ {
		dotProduct += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	cosine := dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
	return (cosine + 1) * 50 // Convert to 0-100%
}
