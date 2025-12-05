package main

import "os"

type Config struct {
	GRPCAddr        string // gateway gRPC port
	HTTPAddr        string // gateway HTTP port
	PlacementDriver string
	JWTSecret       string
	RateLimitRPS    int
}

func LoadConfig() Config {
	return Config{
		GRPCAddr:        getEnv("GRPC_ADDR", ":8081"),
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		PlacementDriver: getEnv("PLACEMENT_DRIVER", "placement:6300"),
		JWTSecret:       getEnv("JWT_SECRET", "CHANGE_ME_IN_PRODUCTION"),
		RateLimitRPS:    100,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
