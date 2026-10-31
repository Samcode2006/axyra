package config

import (
	"fmt"
	"os"
)

// knownDevJWTSecret is the value that shipped in Phase 1. It is public, so a
// deployment that still uses it lets anyone mint valid console tokens.
const knownDevJWTSecret = "axyra-jwt-super-secret-key-phase1"

const minJWTSecretLen = 32

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

	// Seeded console users. A user is only created when its password is set,
	// or (for the demo admin/operator) generated and logged once at first start.
	AdminUsername    string
	AdminPassword    string
	OperatorUsername string
	OperatorPassword string

	// Warnings collected during Load for the caller to log.
	Warnings []string
}

// Load reads configuration from environment variables and validates required values.
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:       getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5434/axyra_platform?sslmode=disable"),
		Port:              getEnv("PORT", "8081"),
		JWTSecret:         getEnv("JWT_SECRET", knownDevJWTSecret),
		AnalyticsURL:      getEnv("ANALYTICS_URL", "http://localhost:8082"),
		DefaultAgentToken: getEnv("AXYRA_AGENT_TOKEN", "axyra_agent_secret_token_123"),
		AdminUsername:     getEnv("AXYRA_ADMIN_USERNAME", "admin"),
		AdminPassword:     getEnv("AXYRA_ADMIN_PASSWORD", ""),
		OperatorUsername:  getEnv("AXYRA_OPERATOR_USERNAME", "operator"),
		OperatorPassword:  getEnv("AXYRA_OPERATOR_PASSWORD", ""),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < minJWTSecretLen {
		return nil, fmt.Errorf("JWT_SECRET must be at least %d characters", minJWTSecretLen)
	}
	if cfg.JWTSecret == knownDevJWTSecret {
		cfg.Warnings = append(cfg.Warnings,
			"JWT_SECRET is the public development default; set a unique value before exposing this service")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
