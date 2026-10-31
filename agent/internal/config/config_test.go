package config

import (
	"os"
	"testing"
)

func TestConfigLoadWithEnv(t *testing.T) {
	os.Setenv("AXYRA_ENDPOINT", "http://test-endpoint:8082")
	os.Setenv("AGENT_ID", "agent_custom_001")
	defer func() {
		os.Unsetenv("AXYRA_ENDPOINT")
		os.Unsetenv("AGENT_ID")
	}()

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if cfg.AxyraEndpoint != "http://test-endpoint:8082" {
		t.Errorf("expected AxyraEndpoint 'http://test-endpoint:8082', got '%s'", cfg.AxyraEndpoint)
	}
	if cfg.AgentID != "agent_custom_001" {
		t.Errorf("expected AgentID 'agent_custom_001', got '%s'", cfg.AgentID)
	}
}
