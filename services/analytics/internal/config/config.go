package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL     string
	RedisURL        string
	ControlPlaneURL string
	Port            string
	ServiceToken    string
	// MaxConcurrentQueries is the platform-wide default; tenants with an
	// explicit quota override this value.
	MaxConcurrentQueries int
}

func Load() (*Config, error) {
	maxConc := 5
	if v := os.Getenv("MAX_CONCURRENT_QUERIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxConc = n
		}
	}

	cfg := &Config{
		DatabaseURL:          getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5434/axyra_platform?sslmode=disable"),
		RedisURL:             getEnv("REDIS_URL", "redis://localhost:6379"),
		ControlPlaneURL:      getEnv("CONTROL_PLANE_URL", "http://localhost:8081"),
		Port:                 getEnv("PORT", "8082"),
		ServiceToken:         os.Getenv("AXYRA_SERVICE_TOKEN"),
		MaxConcurrentQueries: maxConc,
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.ServiceToken) < 32 {
		return nil, fmt.Errorf("AXYRA_SERVICE_TOKEN must be at least 32 characters")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
