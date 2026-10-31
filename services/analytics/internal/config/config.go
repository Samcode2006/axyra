package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL     string
	RedisURL        string
	ControlPlaneURL string
	Port            string
}

func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5434/axyra_platform?sslmode=disable"),
		RedisURL:        getEnv("REDIS_URL", "redis://localhost:6379"),
		ControlPlaneURL: getEnv("CONTROL_PLANE_URL", "http://localhost:8081"),
		Port:            getEnv("PORT", "8082"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

