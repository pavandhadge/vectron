// This file defines the configuration for the API Gateway service.
// It includes settings for server addresses, connection to the placement driver,
// and security parameters like JWT secret and rate limiting.

package main

import "os"

// Config holds all the configuration settings for the API Gateway.
type Config struct {
	GRPCAddr        string // Address for the gRPC server to listen on.
	HTTPAddr        string // Address for the HTTP/JSON gateway to listen on.
	PlacementDriver string // Address of the placement driver service.
	JWTSecret       string // Secret key for signing and verifying JWT tokens.
	RateLimitRPS    int    // Requests per second for the rate limiter.
}

// LoadConfig loads the configuration from environment variables with default fallbacks.
func LoadConfig() Config {
	return Config{
		GRPCAddr:        getEnv("GRPC_ADDR", ":8081"),
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		PlacementDriver: getEnv("PLACEMENT_DRIVER", "placement:6300"),
		JWTSecret:       getEnv("JWT_SECRET", "CHANGE_ME_IN_PRODUCTION"),
		RateLimitRPS:    100,
	}
}

// getEnv retrieves an environment variable by key, returning a fallback value if not set.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
