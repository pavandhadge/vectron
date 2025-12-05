package main

import "os"

type Config struct {
	ListenAddr      string
	PlacementDriver string // e.g. "placement:6300"
	JWTSecret       string
	RateLimitRPS    int
}

func LoadConfig() Config {
	return Config{
		ListenAddr:      getEnv("LISTEN_ADDR", ":8080"),
		PlacementDriver: getEnv("PLACEMENT_DRIVER", "localhost:6300"),
		JWTSecret:       getEnv("JWT_SECRET", "change-me-in-production"),
		RateLimitRPS:    100,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
