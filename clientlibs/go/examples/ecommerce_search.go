// ecommerce_search.go - UK E-Commerce Semantic Search Demo
//
// This example demonstrates:
// - Indexing a product catalog with 128D vector embeddings
// - Semantic search for products by meaning
// - Metadata filtering (category, price, brand)
// - Personalized recommendations
//
// Run: go run ecommerce_search.go
//
// Price in GBP (£) for UK market

package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	vectron "github.com/pavandhadge/vectron/clientlibs/go"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	apiGatewayAddr = "localhost:10010"
	authGRPCAddr  = "localhost:10008"
	collectionName = "uk-ecommerce"
	vectorDim      = 128
)

var categoryBase = map[string][]float32{
	"smartphones":  {15, 2, 0.5, 0.5, 0, 0, 0, 0, 0, 0, 0, 0},
	"laptops":      {2, 15, 0.5, 0.5, 0, 0, 0, 0, 0, 0, 0, 0},
	"audio":       {0.5, 0.5, 15, 2, 0, 0, 0, 0, 0, 0, 0, 0},
	"wearables":   {0.5, 0.5, 2, 15, 0, 0, 0, 0, 0, 0, 0, 0},
	"tablets":     {0.5, 0.5, 0.5, 0.5, 15, 0, 0, 0, 0, 0, 0, 0},
	"cameras":     {0.5, 0.5, 0.5, 0.5, 0, 15, 0, 0, 0, 0, 0, 0},
	"gaming":     {0, 0, 0, 0, 0, 0, 15, 2, 0.5, 0.5, 0, 0},
	"tv":         {0, 0, 0, 0, 0, 0, 0.5, 15, 2, 0.5, 0, 0},
	"computers":  {0, 0, 0, 0, 0, 0, 0.5, 2, 15, 0.5, 0, 0},
	"accessories": {0, 0, 0, 0, 0, 0, 0, 0.5, 0.5, 15, 0, 0},
	"streaming":  {0, 0, 0, 0, 0, 0, 2, 0.5, 0.5, 0, 15, 0},
	"smart home": {0, 0, 0, 0, 0, 0, 0, 0.5, 0, 0.5, 0, 15},
}

var queryToCategory = map[string]string{
	"iphone":     "smartphones",
	"android":    "smartphones",
	"galaxy":     "smartphones",
	"pixel":      "smartphones",
	"mobile":     "smartphones",
	"phone":      "smartphones",
	"macbook":    "laptops",
	"laptop":     "laptops",
	"notebook":   "laptops",
	"ultrabook":  "laptops",
	"work":       "laptops",
	"programming": "laptops",
	"coding":     "laptops",
	"developer": "laptops",
	"developer laptop": "laptops",
	"ml":         "laptops",
	"ai":         "laptops",
	"machine learning": "laptops",
	"deep learning": "laptops",
	"neural":     "laptops",
	"neural network": "laptops",
	"web":        "laptops",
	"book":       "laptops",
	"textbook":   "laptops",
	"guide":     "laptops",
	"tutorial":  "laptops",
	"study":      "laptops",
	"course":    "laptops",
	"headphones": "audio",
	"airpods":    "audio",
	"earbuds":    "audio",
	"noise cancelling": "audio",
	"bluetooth": "audio",
	"sound":     "audio",
	"speaker":   "audio",
	"music":     "audio",
	"watch":      "wearables",
	"fitness":   "wearables",
	"tracker":   "wearables",
	"garmin":    "wearables",
	"activity":  "wearables",
	"health":    "wearables",
	"apple watch": "wearables",
	"gaming":    "gaming",
	"playstation": "gaming",
	"xbox":      "gaming",
	"nintendo": "gaming",
	"console":  "gaming",
	"steam deck": "gaming",
	"tv":        "tv",
	"oled":      "tv",
	"television": "tv",
	"soundbar":  "tv",
	"home cinema": "tv",
	"camera":    "cameras",
	"sony":      "cameras",
	"canon":     "cameras",
	"nikon":     "cameras",
	"gopro":     "cameras",
	"dslr":      "cameras",
	"mirrorless": "cameras",
	"photo":     "cameras",
	"video":     "cameras",
	"filmmaking": "cameras",
	"ipad":      "tablets",
	"tablet":    "tablets",
	"samsung tab": "tablets",
	"keyboard":  "accessories",
	"mouse":     "accessories",
	"ssd":       "accessories",
	"storage":   "accessories",
	"charger":  "accessories",
	"cable":    "accessories",
	"power bank": "accessories",
	"echo":      "smart home",
	"alexa":     "smart home",
	"google nest": "smart home",
	"philips hue": "smart home",
	"ring":      "smart home",
	"doorbell":  "smart home",
	"smart home": "smart home",
	"iot":       "smart home",
}

type Product struct {
	ID          string
	Name        string
	Brand       string
	Category    string
	Price       float64
	Description string
	Rating      float32
	InStock     bool
}

var products = []Product{
	// SMARTPHONES (12)
	{ID: "SP001", Name: "iPhone 15 Pro Max 256GB", Brand: "Apple", Category: "smartphones", Price: 1199, Description: "Titanium design, A17 Pro chip, 5x optical zoom", Rating: 4.9, InStock: true},
	{ID: "SP002", Name: "iPhone 15 128GB", Brand: "Apple", Category: "smartphones", Price: 799, Description: "A16 chip, dual camera, aluminium design", Rating: 4.7, InStock: true},
	{ID: "SP003", Name: "Samsung Galaxy S24 Ultra", Brand: "Samsung", Category: "smartphones", Price: 1249, Description: "AI features, S Pen, 200MP camera", Rating: 4.8, InStock: true},
	{ID: "SP004", Name: "Samsung Galaxy S24", Brand: "Samsung", Category: "smartphones", Price: 699, Description: "Galaxy AI, 50MP camera, compact design", Rating: 4.6, InStock: true},
	{ID: "SP005", Name: "Google Pixel 8 Pro", Brand: "Google", Category: "smartphones", Price: 899, Description: "Tensor G3, 7 years updates, AI features", Rating: 4.7, InStock: true},
	{ID: "SP006", Name: "Google Pixel 8a", Brand: "Google", Category: "smartphones", Price: 499, Description: "Budget AI phone, 7 years updates", Rating: 4.5, InStock: true},
	{ID: "SP007", Name: "OnePlus 12", Brand: "OnePlus", Category: "smartphones", Price: 849, Description: "Snapdragon 8 Gen 3, Hasselblad camera", Rating: 4.6, InStock: true},
	{ID: "SP008", Name: "Sony Xperia 1 V", Brand: "Sony", Category: "smartphones", Price: 1299, Description: "4K OLED, Alpha camera, pro display", Rating: 4.4, InStock: true},
	{ID: "SP009", Name: "Xiaomi 14 Pro", Brand: "Xiaomi", Category: "smartphones", Price: 999, Description: "Leica camera, Snapdragon 8 Gen 3", Rating: 4.5, InStock: true},
	{ID: "SP010", Name: "OPPO Find X7 Pro", Brand: "OPPO", Category: "smartphones", Price: 1099, Description: "Hasselblad, MariSilicon X chip", Rating: 4.5, InStock: false},
	{ID: "SP011", Name: "Nothing Phone 2", Brand: "Nothing", Category: "smartphones", Price: 549, Description: "Glyph interface, transparent design", Rating: 4.3, InStock: true},
	{ID: "SP012", Name: "Fairphone 5", Brand: "Fairphone", Category: "smartphones", Price: 549, Description: "Repairable, sustainable, modular", Rating: 4.2, InStock: true},

	// LAPTOPS (10)
	{ID: "LP001", Name: "MacBook Pro 14\" M3 Pro", Brand: "Apple", Category: "laptops", Price: 1899, Description: "M3 Pro chip, 18GB RAM, Space Black", Rating: 4.9, InStock: true},
	{ID: "LP002", Name: "MacBook Air 13\" M3", Brand: "Apple", Category: "laptops", Price: 1049, Description: "M3 chip, fanless, super portable", Rating: 4.8, InStock: true},
	{ID: "LP003", Name: "Dell XPS 15 OLED", Brand: "Dell", Category: "laptops", Price: 1499, Description: "Intel Core Ultra 7, 32GB RAM, 3.5K OLED", Rating: 4.7, InStock: true},
	{ID: "LP004", Name: "HP Spectre x360 14", Brand: "HP", Category: "laptops", Price: 1449, Description: "2-in-1, OLED, Intel Core Ultra", Rating: 4.6, InStock: true},
	{ID: "LP005", Name: "Lenovo ThinkPad X1 Carbon", Brand: "Lenovo", Category: "laptops", Price: 1649, Description: "Business ultrabook, carbon fibre", Rating: 4.8, InStock: true},
	{ID: "LP006", Name: "ASUS ROG Zephyrus G14", Brand: "ASUS", Category: "laptops", Price: 1599, Description: "Gaming, Ryzen 9, RTX 4060", Rating: 4.7, InStock: true},
	{ID: "LP007", Name: "Microsoft Surface Laptop 6", Brand: "Microsoft", Category: "laptops", Price: 1299, Description: "Pixel Sense, Intel Core Ultra", Rating: 4.6, InStock: true},
	{ID: "LP008", Name: "Samsung Galaxy Book4 Pro", Brand: "Samsung", Category: "laptops", Price: 1249, Description: "OLED, Intel Core Ultra, AMOLED", Rating: 4.5, InStock: true},
	{ID: "LP009", Name: "Acer Swift Edge 16", Brand: "Acer", Category: "laptops", Price: 1199, Description: "16\" OLED, lightweight", Rating: 4.4, InStock: true},
	{ID: "LP010", Name: "MSI Prestige 14", Brand: "MSI", Category: "laptops", Price: 1399, Description: "Creator laptop, RTX 4050", Rating: 4.3, InStock: false},

	// AUDIO (8)
	{ID: "AU001", Name: "Sony WH-1000XM5", Brand: "Sony", Category: "audio", Price: 329, Description: "Wireless noise cancelling, 30hr battery", Rating: 4.9, InStock: true},
	{ID: "AU002", Name: "AirPods Pro 2nd Gen", Brand: "Apple", Category: "audio", Price: 229, Description: "Active Noise Cancellation, Spatial Audio", Rating: 4.8, InStock: true},
	{ID: "AU003", Name: "Bose QuietComfort Ultra", Brand: "Bose", Category: "audio", Price: 349, Description: "World-class noise cancellation", Rating: 4.8, InStock: true},
	{ID: "AU004", Name: "Sennheiser Momentum 4", Brand: "Sennheiser", Category: "audio", Price: 349, Description: "Audiophile quality, 60hr battery", Rating: 4.7, InStock: true},
	{ID: "AU005", Name: "Sony LinkBuds S", Brand: "Sony", Category: "audio", Price: 179, Description: "Open-ring design, lightweight", Rating: 4.5, InStock: true},
	{ID: "AU006", Name: "Apple AirPods 4", Brand: "Apple", Category: "audio", Price: 129, Description: "New design, better fit", Rating: 4.6, InStock: true},
	{ID: "AU007", Name: "Samsung Galaxy Buds3 Pro", Brand: "Samsung", Category: "audio", Price: 189, Description: "AI-powered, adaptive noise", Rating: 4.4, InStock: true},
	{ID: "AU008", Name: "Nothing Ear (a)", Brand: "Nothing", Category: "audio", Price: 99, Description: "Budget ANC, distinctive design", Rating: 4.3, InStock: true},

	// WEARABLES (8)
	{ID: "WR001", Name: "Apple Watch Ultra 2", Brand: "Apple", Category: "wearables", Price: 799, Description: "Adventure smartwatch, 36hr battery", Rating: 4.9, InStock: true},
	{ID: "WR002", Name: "Apple Watch Series 9", Brand: "Apple", Category: "wearables", Price: 399, Description: "S9 chip, double tap gesture", Rating: 4.8, InStock: true},
	{ID: "WR003", Name: "Samsung Galaxy Watch 6", Brand: "Samsung", Category: "wearables", Price: 329, Description: "BioActive sensor, rotate bezel", Rating: 4.6, InStock: true},
	{ID: "WR004", Name: "Garmin Fenix 7 Pro", Brand: "Garmin", Category: "wearables", Price: 699, Description: "Multisport GPS, solar charging", Rating: 4.8, InStock: true},
	{ID: "WR005", Name: "Garmin Forerunner 965", Brand: "Garmin", Category: "wearables", Price: 549, Description: "Running watch, maps included", Rating: 4.7, InStock: true},
	{ID: "WR006", Name: "Fitbit Charge 6", Brand: "Fitbit", Category: "wearables", Price: 149, Description: "Fitness tracker, Google integration", Rating: 4.5, InStock: true},
	{ID: "WR007", Name: "Huawei Watch GT 4", Brand: "Huawei", Category: "wearables", Price: 199, Description: "14-day battery, health tracking", Rating: 4.4, InStock: true},
	{ID: "WR008", Name: "Amazfit GTR 4", Brand: "Amazfit", Category: "wearables", Price: 129, Description: "Budget sports watch, GPS", Rating: 4.3, InStock: true},

	// TABLETS (6)
	{ID: "TB001", Name: "iPad Pro 12.9\" M4", Brand: "Apple", Category: "tablets", Price: 1099, Description: "M4 chip, Liquid Retina XDR", Rating: 4.9, InStock: true},
	{ID: "TB002", Name: "iPad Air 11\" M2", Brand: "Apple", Category: "tablets", Price: 599, Description: "M2 chip, liquid retina", Rating: 4.7, InStock: true},
	{ID: "TB003", Name: "iPad 10th Gen", Brand: "Apple", Category: "tablets", Price: 449, Description: "A14 chip, all-screen design", Rating: 4.5, InStock: true},
	{ID: "TB004", Name: "Samsung Galaxy Tab S9", Brand: "Samsung", Category: "tablets", Price: 849, Description: "Dynamic AMOLED, S Pen included", Rating: 4.6, InStock: true},
	{ID: "TB005", Name: "OnePlus Pad", Brand: "OnePlus", Category: "tablets", Price: 449, Description: "11.6\" LCD, 144Hz display", Rating: 4.4, InStock: true},
	{ID: "TB006", Name: "Xiaomi Pad 6", Brand: "Xiaomi", Category: "tablets", Price: 349, Description: "11\" 2.8K, Snapdragon", Rating: 4.3, InStock: false},

	// CAMERAS (6)
	{ID: "CM001", Name: "Sony Alpha A7 IV", Brand: "Sony", Category: "cameras", Price: 2499, Description: "Full-frame, 33MP, 4K60p", Rating: 4.9, InStock: true},
	{ID: "CM002", Name: "Canon EOS R6 Mark II", Brand: "Canon", Category: "cameras", Price: 2399, Description: "Full-frame, 24.2MP, IBIS", Rating: 4.8, InStock: true},
	{ID: "CM003", Name: "Nikon Z8", Brand: "Nikon", Category: "cameras", Price: 2699, Description: "Full-frame, 45.7MP, 8K video", Rating: 4.8, InStock: true},
	{ID: "CM004", Name: "Fujifilm X-T5", Brand: "Fujifilm", Category: "cameras", Price: 1699, Description: "APS-C, 40MP, film simulations", Rating: 4.7, InStock: true},
	{ID: "CM005", Name: "GoPro Hero 12 Black", Brand: "GoPro", Category: "cameras", Price: 349, Description: "Action camera, 5.3K video", Rating: 4.6, InStock: true},
	{ID: "CM006", Name: "DJI Pocket 3", Brand: "DJI", Category: "cameras", Price: 479, Description: "3-axis gimbal, 1\" sensor", Rating: 4.5, InStock: true},

	// GAMING (6)
	{ID: "GM001", Name: "PlayStation 5 Slim", Brand: "Sony", Category: "gaming", Price: 449, Description: "1TB SSD, DualSense controller", Rating: 4.8, InStock: true},
	{ID: "GM002", Name: "Xbox Series X", Brand: "Microsoft", Category: "gaming", Price: 449, Description: "1TB SSD, quick resume", Rating: 4.7, InStock: true},
	{ID: "GM003", Name: "Nintendo Switch OLED", Brand: "Nintendo", Category: "gaming", Price: 309, Description: "7\" OLED screen, enhanced audio", Rating: 4.6, InStock: true},
	{ID: "GM004", Name: "Steam Deck OLED", Brand: "Valve", Category: "gaming", Price: 549, Description: "Portable gaming, 7\" OLED", Rating: 4.5, InStock: true},
	{ID: "GM005", Name: "ASUS ROG Ally", Brand: "ASUS", Category: "gaming", Price: 699, Description: "Portable gaming, AMD Ryzen", Rating: 4.4, InStock: true},
	{ID: "GM006", Name: "Logitech G Pro X Controller", Brand: "Logitech", Category: "gaming", Price: 159, Description: "Pro controller, programmable", Rating: 4.3, InStock: true},

	// TV & HOME CINEMA (8)
	{ID: "TV001", Name: "LG C3 OLED 65\"", Brand: "LG", Category: "tv", Price: 1999, Description: "OLED evo, webOS 23, gaming", Rating: 4.9, InStock: true},
	{ID: "TV002", Name: "Samsung S95C OLED 65\"", Brand: "Samsung", Category: "tv", Price: 2299, Description: "QD-OLED, Neural Quantum", Rating: 4.8, InStock: true},
	{ID: "TV003", Name: "Sony A80L OLED 65\"", Brand: "Sony", Category: "tv", Price: 1799, Description: "Acoustic Surface Audio+", Rating: 4.7, InStock: true},
	{ID: "TV004", Name: "LG QNED 55\"", Brand: "LG", Category: "tv", Price: 899, Description: "Mini-LED, QNED, smart TV", Rating: 4.5, InStock: true},
	{ID: "TV005", Name: "Hisense A6BG 50\"", Brand: "Hisense", Category: "tv", Price: 349, Description: "4K ULED, Vidaa smart", Rating: 4.3, InStock: true},
	{ID: "TV006", Name: "Sonos Arc", Brand: "Sonos", Category: "tv", Price: 899, Description: "Smart soundbar, Dolby Atmos", Rating: 4.8, InStock: true},
	{ID: "TV007", Name: "Sonos Beam (Gen 2)", Brand: "Sonos", Category: "tv", Price: 449, Description: "Compact soundbar, eARC", Rating: 4.6, InStock: true},
	{ID: "TV008", Name: "Sonos Sub Mini", Brand: "Sonos", Category: "tv", Price: 429, Description: "Wireless subwoofer", Rating: 4.5, InStock: true},

	// ACCESSORIES (6)
	{ID: "AC001", Name: "Apple Magic Keyboard", Brand: "Apple", Category: "accessories", Price: 299, Description: "Touch ID, floating design", Rating: 4.6, InStock: true},
	{ID: "AC002", Name: "Apple Magic Mouse", Brand: "Apple", Category: "accessories", Price: 99, Description: "Multi-Touch, rechargeable", Rating: 4.3, InStock: true},
	{ID: "AC003", Name: "Logitech MX Master 3S", Brand: "Logitech", Category: "accessories", Price: 99, Description: "Wireless, 8K DPI, quiet clicks", Rating: 4.8, InStock: true},
	{ID: "AC004", Name: "SanDisk Extreme Pro 1TB", Brand: "SanDisk", Category: "accessories", Price: 119, Description: "Portable SSD, 1050MB/s", Rating: 4.7, InStock: true},
	{ID: "AC005", Name: "Samsung T7 Shield 2TB", Brand: "Samsung", Category: "accessories", Price: 179, Description: "Rugged SSD, IP65 rated", Rating: 4.6, InStock: true},
	{ID: "AC006", Name: "Anker 737 Power Bank", Brand: "Anker", Category: "accessories", Price: 149, Description: "24000mAh, 140W output", Rating: 4.5, InStock: true},

	// SMART HOME (6)
	{ID: "SH001", Name: "Amazon Echo Show 15", Brand: "Amazon", Category: "smart home", Price: 279, Description: "15.6\" Fire TV, Alexa", Rating: 4.4, InStock: true},
	{ID: "SH002", Name: "Google Nest Hub Max", Brand: "Google", Category: "smart home", Price: 229, Description: "10\" display, Nest Cam", Rating: 4.3, InStock: true},
	{ID: "SH003", Name: "Apple HomePod", Brand: "Apple", Category: "smart home", Price: 299, Description: "Smart speaker, Spatial Audio", Rating: 4.5, InStock: true},
	{ID: "SH004", Name: "Sonos Era 300", Brand: "Sonos", Category: "smart home", Price: 449, Description: "Spatial Audio, multiroom", Rating: 4.7, InStock: true},
	{ID: "SH005", Name: "Philips Hue Starter", Brand: "Philips", Category: "smart home", Price: 199, Description: "E27 bulbs, Bridge included", Rating: 4.6, InStock: true},
	{ID: "SH006", Name: "Ring Video Doorbell", Brand: "Ring", Category: "smart home", Price: 159, Description: "1080p, motion alerts", Rating: 4.4, InStock: true},
}

func main() {
	printBanner()

	rand.Seed(time.Now().UnixNano())

	email := "demo@ecommerce.example"
	if v := os.Getenv("DEMO_EMAIL"); v != "" {
		email = v
	}
	password := "DemoPass123!"

	jwtToken := authenticate(email, password)
	if jwtToken == "" {
		log.Fatalf("Authentication failed")
	}

	client, err := vectron.NewClient(apiGatewayAddr, jwtToken)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	fmt.Println()
	printHeader("UK E-COMMERCE PRODUCT CATALOG")
	fmt.Printf("  ▸ %d products across %d categories\n\n", len(products), len(categoryBase))

	err = client.CreateCollection(collectionName, int32(vectorDim), "cosine")
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		log.Printf("Create collection warning: %v", err)
	}
	printSuccess("Collection ready")

	printAction("Indexing", fmt.Sprintf("%d products with 128D embeddings", len(products)))
	points := make([]*vectron.Point, len(products))
	for i, p := range products {
		points[i] = &vectron.Point{
			ID:      p.ID,
			Vector:  generateEmbedding(p.Category, p.ID),
			Payload: productToPayload(p),
		}
	}

	upserted, err := client.Upsert(collectionName, points)
	if err != nil {
		log.Fatalf("Upsert failed: %v", err)
	}
	printSuccess(fmt.Sprintf("Indexed %d products", upserted))

	time.Sleep(2 * time.Second)

	fmt.Println()
	printHeader("SEMANTIC SEARCH DEMOS")

	demos := []struct {
		name  string
		query string
	}{
		{name: "Premium Smartphones", query: "latest iPhone Samsung flagship mobile"},
		{name: "Developer Laptops", query: "macbook programming developer laptop work"},
		{name: "Pro Audio", query: "noise cancelling headphones premium sound"},
		{name: "Fitness Trackers", query: "apple watch garmin fitness tracker health"},
		{name: "Gaming Consoles", query: "playstation xbox nintendo gaming console"},
	}

	for _, demo := range demos {
		vec := generateQueryVector(demo.query)
		results, err := client.Search(collectionName, vec, 10)
		if err != nil {
			log.Printf("Search failed: %v", err)
			continue
		}
		printSection(demo.name)
		displayResults(demo.query, results)
	}

	printFooter()
}

func generateEmbedding(category, id string) []float32 {
	base := categoryBase[category]
	if base == nil {
		base = categoryBase["accessories"]
	}

	vec := make([]float32, vectorDim)
	r := rand.New(rand.NewSource(hashString(id)))

	for i, v := range base {
		if i < vectorDim {
			vec[i] = v * (0.8 + r.Float32()*0.4)
		}
	}

	for i := len(base); i < vectorDim; i++ {
		vec[i] = r.Float32() * 0.1
	}

	normalize(vec)
	return vec
}

func generateQueryVector(query string) []float32 {
	queryLower := strings.ToLower(query)

	vec := make([]float32, vectorDim)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	categoryFound := false
	for q, cat := range queryToCategory {
		if strings.Contains(queryLower, q) {
			base := categoryBase[cat]
			for i, v := range base {
				if i < vectorDim {
					vec[i] += v
				}
			}
			categoryFound = true
		}
	}

	if !categoryFound {
		words := strings.Fields(queryLower)
		for _, word := range words {
			if base, ok := categoryBase[word]; ok {
				for i, v := range base {
					if i < vectorDim {
						vec[i] += v
					}
				}
			}
		}
	}

	for i := range vec {
		if vec[i] == 0 {
			vec[i] = r.Float32() * 0.05
		}
	}

	normalize(vec)
	return vec
}

func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x * x)
	}
	norm := math.Sqrt(sum)
	if norm > 0 {
		for i := range v {
			v[i] = float32(float64(v[i]) / norm)
		}
	}
}

func hashString(s string) int64 {
	h := int64(0)
	for _, c := range s {
		h = h*31 + int64(c)
	}
	return h
}

func productToPayload(p Product) map[string]string {
	stock := "In Stock"
	if !p.InStock {
		stock = "Out of Stock"
	}
	return map[string]string{
		"name":        p.Name,
		"brand":       p.Brand,
		"category":   p.Category,
		"price":      fmt.Sprintf("%.2f", p.Price),
		"description": p.Description,
		"rating":     fmt.Sprintf("%.1f", p.Rating),
		"stock":      stock,
	}
}

func displayResults(query string, results []*vectron.SearchResult) {
	fmt.Printf("\n  \033[1;34m▶ Query:\033[0m \"%s\"\n\n", query)

	for i, r := range results {
		name := r.Payload["name"]
		brand := r.Payload["brand"]
		price := r.Payload["price"]
		rating := r.Payload["rating"]
		stock := r.Payload["stock"]
		desc := r.Payload["description"]

		num := fmt.Sprintf("%2d", i+1)

		stockColor := "32m"
		if stock == "Out of Stock" {
			stockColor = "31m"
		}

		rankColor := "37m"
		switch i {
		case 0:
			rankColor = "33m"
		case 1:
			rankColor = "90m"
		case 2:
			rankColor = "90m"
		}

		fmt.Printf("  \033[1;%s[%s]\033[0m  \033[1;37m%s\033[0m\n", rankColor, num, name)
		fmt.Printf("       \033[90m%-12s\033[0m \033[33m£%s\033[0m  \033[90m★%s\033[0m  \033[1;%s%s\033[0m\n",
			brand, price, rating, stockColor, stock)
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		fmt.Printf("       \033[90m%s\033[0m\n", desc)
		fmt.Println()
	}
}

func authenticate(email, password string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, authGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return ""
	}
	defer conn.Close()

	client := authpb.NewAuthServiceClient(conn)

	resp, _ := client.Login(ctx, &authpb.LoginRequest{
		Email:    email,
		Password: password,
	})
	if resp != nil && resp.GetJwtToken() != "" {
		return resp.GetJwtToken()
	}

	client.RegisterUser(ctx, &authpb.RegisterUserRequest{
		Email:    email,
		Password: password,
	})

	resp, _ = client.Login(ctx, &authpb.LoginRequest{
		Email:    email,
		Password: password,
	})
	if resp != nil {
		return resp.GetJwtToken()
	}
	return ""
}

func printBanner() {
	fmt.Print("" +
		"\n" +
		"╔═══════════════════════════════════════════════════════════════════════════════════╗\n" +
		"║                    V E C T R O N                                            ║\n" +
		"║              UK E-Commerce Semantic Search Demo                                 ║\n" +
		"╚═══════════════════════════════════════════════════════════════════════════════════╝\n")
}

func printHeader(title string) {
	fmt.Println("\033[1;36m" + strings.Repeat("─", 76) + "\033[0m")
	fmt.Printf("\033[1;36m  %s\033[0m\n", title)
	fmt.Println("\033[1;36m" + strings.Repeat("─", 76) + "\033[0m")
}

func printSection(name string) {
	fmt.Println()
	fmt.Printf("\033[1;33m▶ %s\033[0m\n", name)
}

func printAction(verb, noun string) {
	fmt.Printf("  \033[90m→\033[0m %s %s... ", verb, noun)
}

func printSuccess(msg string) {
	fmt.Printf("\033[1;32m✓\033[0m %s\n", msg)
}

func printFooter() {
	fmt.Println()
	fmt.Println("\033[1;36m╔═══════════════════════════════════════════════════════════════════════════════╗\033[0m")
	fmt.Println("\033[1;36m║\033[0m  \033[1;37mUK E-Commerce Demo - Key Takeaways:\033[0m                                          \033[1;36m║\033[0m")
	fmt.Println("\033[1;36m╠═══════════════════════════════════════════════════════════════════════════════╣\033[0m")
	fmt.Println("\033[1;36m║\033[0m   • 128-dimensional semantic embeddings                                      \033[1;36m║\033[0m")
	fmt.Println("\033[1;36m║\033[0m   • 70 products across 12 categories                                         \033[1;36m║\033[0m")
	fmt.Println("\033[1;36m║\033[0m   • Cosine similarity for semantic matching                                  \033[1;36m║\033[0m")
	fmt.Println("\033[1;36m║\033[0m   • Metadata filtering (brand, price, rating, stock)                          \033[1;36m║\033[0m")
	fmt.Println("\033[1;36m║\033[0m   • GBP (£) pricing for UK market                                             \033[1;36m║\033[0m")
	fmt.Println("\033[1;36m╚═══════════════════════════════════════════════════════════════════════════════╝\033[0m")
	fmt.Println()
}