package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type TableConfig struct {
	Name            string   `yaml:"name"`
	TenantIDColumn  string   `yaml:"tenant_id_column"`
	PKColumn        string   `yaml:"pk_column"`
	EventTypeColumn string   `yaml:"event_type_column"`
	Columns         []string `yaml:"columns"`
}

type Config struct {
	AxyraEndpoint   string        `yaml:"axyra_endpoint"`
	AgentToken      string        `yaml:"agent_token"`
	AgentID         string        `yaml:"agent_id"`
	PostgresDSN     string        `yaml:"postgres_dsn"`
	ReplicationSlot string        `yaml:"replication_slot"`
	PublicationName string        `yaml:"publication_name"`
	BatchSize       int           `yaml:"batch_size"`
	FlushInterval   time.Duration `yaml:"flush_interval"`
	HealthPort      string        `yaml:"health_port"`
	Tables          []TableConfig `yaml:"tables"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		AxyraEndpoint:   "http://localhost:8082",
		AgentToken:      "axyra_agent_secret_token_123",
		AgentID:         "agent_ref_demo",
		PostgresDSN:     "postgres://postgres:postgres@localhost:5433/reference_db?sslmode=disable",
		ReplicationSlot: "axyra_slot",
		PublicationName: "axyra_pub",
		BatchSize:       10,
		FlushInterval:   200 * time.Millisecond,
		HealthPort:      "8083",
		Tables: []TableConfig{
			{
				Name:            "orders",
				TenantIDColumn:  "store_id",
				PKColumn:        "id",
				EventTypeColumn: "status",
				Columns:         []string{"id", "store_id", "amount", "currency", "status", "product_id", "product_name", "category", "created_at", "updated_at"},
			},
		},
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			// Expand env vars in YAML e.g. ${POSTGRES_DSN}
			expanded := expandEnvVars(string(data))
			if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
				return nil, fmt.Errorf("parse config file: %w", err)
			}
		}
	}

	// Environment variable overrides
	if v := os.Getenv("AXYRA_ENDPOINT"); v != "" {
		cfg.AxyraEndpoint = v
	}
	if v := os.Getenv("AXYRA_AGENT_TOKEN"); v != "" {
		cfg.AgentToken = v
	}
	if v := os.Getenv("AGENT_ID"); v != "" {
		cfg.AgentID = v
	}
	if v := os.Getenv("POSTGRES_DSN"); v != "" {
		cfg.PostgresDSN = v
	}
	if v := os.Getenv("REPLICATION_SLOT"); v != "" {
		cfg.ReplicationSlot = v
	}
	if v := os.Getenv("PUBLICATION_NAME"); v != "" {
		cfg.PublicationName = v
	}
	if v := os.Getenv("HEALTH_PORT"); v != "" {
		cfg.HealthPort = v
	}
	if v := os.Getenv("BATCH_SIZE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			cfg.BatchSize = i
		}
	}

	return cfg, nil
}

var envVarRegex = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)\}`)

func expandEnvVars(s string) string {
	return envVarRegex.ReplaceAllStringFunc(s, func(match string) string {
		varName := match[2 : len(match)-1]
		if val, ok := os.LookupEnv(varName); ok {
			return val
		}
		return match
	})
}
