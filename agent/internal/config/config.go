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

// SourceConfig describes one tenant database the agent should replicate from.
type SourceConfig struct {
	// Name is a human-readable label for this source, used in log lines.
	Name string `yaml:"name"`
	// ExternalTenantID is the Axyra tenant ID that all events from this source
	// will be tagged with. When empty the mapper falls back to reading the
	// tenant_id_column from each row (backward-compatible shared-DB behaviour).
	ExternalTenantID string `yaml:"external_tenant_id"`
	// PostgresDSN is the connection string for this tenant's database.
	PostgresDSN string `yaml:"postgres_dsn"`
	// ReplicationSlot is the logical replication slot name. Defaults to
	// "axyra_slot_<name>" when empty.
	ReplicationSlot string `yaml:"replication_slot"`
	// PublicationName is the PostgreSQL publication name. Defaults to "axyra_pub".
	PublicationName string `yaml:"publication_name"`
}

type Config struct {
	EventIDNamespace string `yaml:"event_id_namespace"`
	AxyraEndpoint    string `yaml:"axyra_endpoint"`
	AgentToken       string `yaml:"agent_token"`
	AgentID          string `yaml:"agent_id"`
	// Legacy single-DB fields. Used when Sources is empty.
	PostgresDSN     string        `yaml:"postgres_dsn"`
	ReplicationSlot string        `yaml:"replication_slot"`
	PublicationName string        `yaml:"publication_name"`
	BatchSize       int           `yaml:"batch_size"`
	FlushInterval   time.Duration `yaml:"flush_interval"`
	HealthPort      string        `yaml:"health_port"`
	Tables          []TableConfig `yaml:"tables"`
	// Sources is the list of tenant databases to replicate from. When non-empty
	// it takes precedence over the legacy PostgresDSN/ReplicationSlot fields.
	Sources []SourceConfig `yaml:"sources"`
}

// EffectiveSources returns the list of sources the agent should connect to.
// Priority:
//  1. Sources declared in YAML (cfg.Sources)
//  2. Sources auto-generated from TENANT_COUNT env var
//  3. Legacy single PostgresDSN (backward compat)
func (cfg *Config) EffectiveSources() []SourceConfig {
	if len(cfg.Sources) > 0 {
		// Fill in defaults for each source
		for i := range cfg.Sources {
			s := &cfg.Sources[i]
			if s.ReplicationSlot == "" {
				s.ReplicationSlot = "axyra_slot_" + s.Name
			}
			if s.PublicationName == "" {
				s.PublicationName = "axyra_pub"
			}
		}
		return cfg.Sources
	}

	// Auto-generate from TENANT_COUNT
	if countStr := os.Getenv("TENANT_COUNT"); countStr != "" {
		n, err := strconv.Atoi(countStr)
		if err == nil && n > 0 {
			sources := make([]SourceConfig, n)
			for i := 0; i < n; i++ {
				num := i + 1
				name := fmt.Sprintf("store_%03d", num)
				sources[i] = SourceConfig{
					Name:             name,
					ExternalTenantID: name,
					PostgresDSN:      fmt.Sprintf("postgres://postgres:postgres@reference-db-%d:5432/reference_db?sslmode=disable", num),
					ReplicationSlot:  fmt.Sprintf("axyra_slot_%s", name),
					PublicationName:  "axyra_pub",
				}
			}
			return sources
		}
	}

	// Legacy single-source fallback
	slot := cfg.ReplicationSlot
	if slot == "" {
		slot = "axyra_slot"
	}
	pub := cfg.PublicationName
	if pub == "" {
		pub = "axyra_pub"
	}
	sourceName := "default"
	if v := os.Getenv("SOURCE_TENANT_ID"); v != "" {
		sourceName = v
	}
	return []SourceConfig{
		{
			Name:             sourceName,
			ExternalTenantID: os.Getenv("SOURCE_TENANT_ID"),
			PostgresDSN:      cfg.PostgresDSN,
			ReplicationSlot:  slot,
			PublicationName:  pub,
			// ExternalTenantID left empty — mapper reads from row
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		EventIDNamespace: os.Getenv("EVENT_ID_NAMESPACE"),
		AxyraEndpoint:    "http://localhost:8082",
		AgentToken:       "axyra_agent_secret_token_123",
		AgentID:          "agent_ref_demo",
		PostgresDSN:      "postgres://postgres:postgres@localhost:5433/reference_db?sslmode=disable",
		ReplicationSlot:  "axyra_slot",
		PublicationName:  "axyra_pub",
		BatchSize:        10,
		FlushInterval:    200 * time.Millisecond,
		HealthPort:       "8083",
		Tables: []TableConfig{
			{
				Name:            "orders",
				TenantIDColumn:  "store_id",
				PKColumn:        "id",
				EventTypeColumn: "status",
				// 27 columns: the 8 base order columns, 17 extended ones (schema v2) and the
				// two timestamps. customer_email is never sent to the platform.
				Columns: []string{
					"id",
					"store_id",
					"amount",
					"currency",
					"status",
					"product_id",
					"product_name",
					"category",
					"customer_id",
					"customer_tier",
					"payment_method",
					"payment_status",
					"discount_code",
					"discount_amount",
					"tax_amount",
					"shipping_cost",
					"quantity",
					"sku",
					"fulfillment_status",
					"return_reason",
					"country",
					"state_province",
					"city",
					"channel",
					"referrer_source",
					"created_at",
					"updated_at",
				},
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
