package config

import "testing"

func TestManagedSource(t *testing.T) {
	t.Setenv("TENANT_COUNT", "")
	t.Setenv("SOURCE_TENANT_ID", "store_new")
	t.Setenv("EVENT_ID_NAMESPACE", "tenant_new")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	sources := cfg.EffectiveSources()
	if len(sources) != 1 || sources[0].Name != "store_new" || sources[0].ExternalTenantID != "store_new" || cfg.EventIDNamespace != "tenant_new" {
		t.Fatalf("incorrect managed config: %+v", sources)
	}
}
