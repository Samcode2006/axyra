package mapper

import (
	"github.com/axyra/agent/internal/config"
	"github.com/axyra/agent/internal/connector"
	"testing"
)

func TestManagedNamespaces(t *testing.T) {
	event := connector.ChangeEvent{LSN: "0/10", Table: "orders", Operation: "INSERT", Row: map[string]interface{}{"id": "same-order", "store_id": "same-store"}}
	one, err := New(&config.Config{EventIDNamespace: "tenant_a"}).Map(event)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := New(&config.Config{EventIDNamespace: "tenant_a"}).Map(event)
	other, _ := New(&config.Config{EventIDNamespace: "tenant_b"}).Map(event)
	if one.EventID != again.EventID {
		t.Fatal("retry identity is unstable")
	}
	if one.EventID == other.EventID {
		t.Fatal("isolated databases collide")
	}
}
