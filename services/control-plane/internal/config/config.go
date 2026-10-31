package config

import (
	"fmt"
	"os"
)

// Config holds all control-plane configuration sourced from environment variables.
type Config struct {
	// Database
	DatabaseURL string

	// HTTP server
	Port string

	// Auth
	JWTSecret string

	// Analytics backend service URL for reverse-proxying analytics queries from web
	AnalyticsURL string

	// Default agent token to seed if not already present
	DefaultAgentToken string
}

// Load reads configuration from environment variables and validates required values.
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:       getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5434/axyra_platform?sslmode=disable"),
		Port:              getEnv("PORT", "8081"),
		JWTSecret:         getEnv("JWT_SECRET", "axyra-jwt-super-secret-key-phase1"),
		AnalyticsURL:      getEnv("ANALYTICS_URL", "http://localhost:8082"),
		DefaultAgentToken: getEnv("AXYRA_AGENT_TOKEN", "axyra_agent_secret_token_123"),
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
