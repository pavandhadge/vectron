package main

import (
	"context"
	"log"
	"net/http"

	"github.com/pavandhadge/vectron/apigateway/internal/middleware"
	pb "github.com/pavandhadge/vectron/apigateway/proto/apigateway"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var cfg = LoadConfig()

func main() {
	middleware.SetJWTSecret(cfg.JWTSecret)

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create gateway mux
	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions:   runtime.MarshalOptions{EmitDefaults: true},
			UnmarshalOptions: runtime.UnmarshalOptions{DiscardUnknown: true},
		}),
	)

	// Connect to your placement driver (or worker directly for now)
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	err := pb.RegisterVectronServiceHandlerFromEndpoint(
		ctx, mux, cfg.PlacementDriver, opts,
	)
	if err != nil {
		log.Fatalf("Failed to connect to placement driver: %v", err)
	}

	// Middleware chain (order matters!)
	handler := middleware.Logging(
		middleware.RateLimit(cfg.RateLimitRPS)(
			middleware.Auth(mux),
		),
	)

	log.Printf("Vectron API Gateway starting on %s", cfg.ListenAddr)
	log.Printf("Forwarding to placement driver at %s", cfg.PlacementDriver)
	log.Printf("JWT secret loaded, rate limit: %d RPS", cfg.RateLimitRPS)

	log.Fatal(http.ListenAndServe(cfg.ListenAddr, handler))
}
