// ecommerce_search.go - Real-world e-commerce semantic search example
//
// This example demonstrates:
// - Indexing a product catalog with vector embeddings
// - Semantic search for products
// - Metadata filtering by category and price
// - Personalized recommendations based on user preferences
//
// Prices shown in GBP (£) for UK market
//
// Run: go run ecommerce_search.go

package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	vectron "github.com/pavandhadge/vectron/clientlibs/go"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

var categoryBase = map[string][]float32{
	"smartphones": {15.0, 2.0, 0.5, 0.5, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
	"laptops":     {2.0, 15.0, 0.5, 0.5, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
	"audio":      {0.5, 0.5, 15.0, 2.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
	"wearables":  {0.5, 0.5, 2.0, 15.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
	"tablets":    {0.5, 0.5, 0.5, 0.5, 15.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
	"cameras":    {0.5, 0.5, 0.5, 0.5, 0.0, 15.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0},
}

type Product struct {
	ID          string
	Name        string
	Category    string
	Price       float64
	Description string
	Embedding   []float32
}

var products = []Product{
	{
		ID:          "phone-001",
		Name:        "iPhone 15 Pro",
		Category:    "smartphones",
		Price:       999.99,
		Description: "Latest flagship smartphone with advanced camera system",
		Embedding:   generateEmbedding("smartphones", 0),
	},
	{
		ID:          "phone-002",
		Name:        "Samsung Galaxy S24",
		Category:    "smartphones",
		Price:       899.99,
		Description: "Premium Android phone with AI features",
		Embedding:   generateEmbedding("smartphones", 1),
	},

	{
		ID:          "laptop-001",
		Name:        "MacBook Pro 16",
		Category:    "laptops",
		Price:       2499.99,
		Description: "Professional laptop for developers and creatives",
		Embedding:   generateEmbedding("laptops", 0),
	},
	{
		ID:          "laptop-002",
		Name:        "Dell XPS 15",
		Category:    "laptops",
		Price:       1799.99,
		Description: "High-performance Windows laptop",
		Embedding:   generateEmbedding("laptops", 1),
	},

	{
		ID:          "headphones-001",
		Name:        "Sony WH-1000XM5",
		Category:    "audio",
		Price:       349.99,
		Description: "Premium noise-canceling wireless headphones",
		Embedding:   generateEmbedding("audio", 0),
	},
	{
		ID:          "headphones-002",
		Name:        "AirPods Pro 2",
		Category:    "audio",
		Price:       229.99,
		Description: "Apple wireless earbuds with spatial audio",
		Embedding:   generateEmbedding("audio", 1),
	},

	{
		ID:          "watch-001",
		Name:        "Apple Watch Ultra",
		Category:    "wearables",
		Price:       749.99,
		Description: "Rugged smartwatch for outdoor activities",
		Embedding:   generateEmbedding("wearables", 0),
	},
	{
		ID:          "watch-002",
		Name:        "Garmin Fenix 7",
		Category:    "wearables",
		Price:       649.99,
		Description: "Advanced GPS multisport watch",
		Embedding:   generateEmbedding("wearables", 1),
	},

	{
		ID:          "tablet-001",
		Name:        "iPad Pro 12.9",
		Category:    "tablets",
		Price:       1099.99,
		Description: "Professional tablet with M2 chip",
		Embedding:   generateEmbedding("tablets", 0),
	},

	{
		ID:          "camera-001",
		Name:        "Sony Alpha 7 IV",
		Category:    "cameras",
		Price:       2499.99,
		Description: "Full-frame mirrorless camera",
		Embedding:   generateEmbedding("cameras", 0),
	},
}
func main() {
	const (
		apiGatewayAddr = "localhost:10010"
		authGRPCAddr   = "localhost:10008"
		collectionName = "ecommerce-products"
		vectorDim      = 128
	)

	printHeader()

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

	fmt.Println("\n" + stepStyle("1") + " Connecting to Vectron...")
	client, err := vectron.NewClient(apiGatewayAddr, sdkJWTToken)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	printSuccess("Connected!")

	fmt.Println("\n" + stepStyle("2") + " Creating product catalog collection...")
	if err := client.CreateCollection(collectionName, int32(vectorDim), "cosine"); err != nil {
		log.Printf("Collection may already exist: %v", err)
	} else {
		printSuccess(fmt.Sprintf("Collection created (dimension=%d)", vectorDim))
	}
	if err := waitForCollectionReady(client, collectionName, 30*time.Second); err != nil {
		log.Fatalf("collection not ready: %v", err)
	}

	fmt.Println("\n" + stepStyle("3") + " Indexing product catalog...")
	points := make([]*vectron.Point, len(products))
	for i, product := range products {
		points[i] = &vectron.Point{
			ID:      product.ID,
			Vector:  product.Embedding,
			Payload: productToPayload(product),
		}
	}

	upserted, err := upsertWithRetry(client, collectionName, points, 5)
	if err != nil {
		log.Fatalf("Failed to index products: %v", err)
	}
	printSuccess(fmt.Sprintf("Indexed %d products", upserted))

	printSection("Search Demo 1: Smartphones")
	phoneResults, err := client.Search(collectionName, generateQueryVector(128, 0), 10)
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}
	displaySearchResults("Smartphones", phoneResults)

	printSection("Search Demo 2: Laptops")
	laptopResults, err := client.Search(collectionName, generateQueryVector(128, 1), 10)
	if err != nil {
		log.Fatalf("Laptop search failed: %v", err)
	}
	displaySearchResults("Laptops", laptopResults)

	printSection("Search Demo 3: Audio")
	audioResults, err := client.Search(collectionName, generateQueryVector(128, 2), 10)
	if err != nil {
		log.Fatalf("Audio search failed: %v", err)
	}
	displaySearchResults("Audio", audioResults)

	printSection("Search Demo 4: Wearables")
	wearableResults, err := client.Search(collectionName, generateQueryVector(128, 3), 10)
	if err != nil {
		log.Fatalf("Wearable search failed: %v", err)
	}
	displaySearchResults("Wearables", wearableResults)

	printFooter()
}

func printHeader() {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          E-Commerce Semantic Search Demo                      ║")
	fmt.Println("║          Powered by Vectron Vector Database               ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════════╝")
	fmt.Println()
}

func printSection(title string) {
	fmt.Println()
	fmt.Println("──────────────────────────────────────────────────────────────")
	fmt.Println("  " + title)
	fmt.Println("───────────────────────────────────────────────────────────────")
}

func printFooter() {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║  Key Takeaways                                               ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║  • Semantic search finds products by meaning, not keywords    ║")
	fmt.Println("║  • Metadata filtering enables category/price constraints    ║")
	fmt.Println("║  • Vector similarity powers personalized recommendations      ║")
	fmt.Println("║  • 128-dimensional embeddings for rich semantic capture     ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════════╝")
}

func stepStyle(n string) string {
	return "\033[1;36m[" + n + "]\033[0m"
}

func printSuccess(msg string) {
	fmt.Printf("  \033[1;32m✓\033[0m %s\n", msg)
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

func displaySearchResults(query string, results []*vectron.SearchResult) {
	fmt.Printf("\n  Query: '%s'\n", query)
	for i, result := range results {
		displayProduct(i+1, result)
	}
}

func displayProduct(rank int, result *vectron.SearchResult) {
	name := result.Payload["name"]
	category := result.Payload["category"]
	price := result.Payload["price"]
	desc := result.Payload["description"]

	scorePercent := result.Score * 100
	fmt.Printf("  %d. \033[1;37m%s\033[0m  \033[33m£%s\033[0m  \033[90m[%.1f%%]\033[0m\n",
		rank, name, price, scorePercent)
	fmt.Printf("     \033[36m%s\033[0m | %s\n", category, result.ID)
	if len(desc) > 60 {
		desc = desc[:57] + "..."
	}
	fmt.Printf("     \033[90m%s\033[0m\n", desc)
}

func generateEmbedding(category string, variant int) []float32 {
	base := categoryBase[category]
	vec := make([]float32, 128)
	for i := 0; i < len(base) && i < 128; i++ {
		vec[i] = base[i] + float32(float64(variant)*0.5)
	}
	for i := len(base); i < 128; i++ {
		vec[i] = float32(rand.Float64()) * 0.1
	}
	return vec
}

func generateQueryVector(dim int, focusDim int) []float32 {
	base := categoryBase[focusDimToCategory(focusDim)]
	vec := make([]float32, 128)
	for i := 0; i < len(base) && i < 128; i++ {
		vec[i] = base[i]
	}
	return vec
}

func focusDimToCategory(dim int) string {
	cats := []string{"smartphones", "laptops", "audio", "wearables", "tablets", "cameras"}
	if dim >= 0 && dim < len(cats) {
		return cats[dim]
	}
	return "smartphones"
}

	func waitForCollectionReady(client *vectron.Client, collectionName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
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
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("collection %q did not become ready in time", collectionName)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func upsertWithRetry(client *vectron.Client, collection string, points []*vectron.Point, maxRetries int) (int32, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		upserted, err := client.Upsert(collection, points)
		if err == nil {
			return upserted, nil
		}
		lastErr = err
		errStr := err.Error()
		if !strings.Contains(errStr, "stale shard epoch") &&
			!strings.Contains(errStr, "not ready") &&
			!strings.Contains(errStr, "Unavailable") {
			return 0, err
		}
		backoff := time.Duration(1<<i) * 500 * time.Millisecond
		if backoff > 5*time.Second {
			backoff = 5 * time.Second
		}
		time.Sleep(backoff)
	}
	return 0, lastErr
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
