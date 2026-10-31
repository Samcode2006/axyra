package mapper

import (
	"fmt"
	"time"

	"github.com/axyra/agent/internal/config"
	"github.com/axyra/agent/internal/connector"
	"github.com/google/uuid"
)

type AxyraEvent struct {
	EventID          string                 `json:"event_id"`
	AgentID          string                 `json:"agent_id"`
	ExternalTenantID string                 `json:"external_tenant_id"`
	EventType        string                 `json:"event_type"`
	SchemaVersion    int                    `json:"schema_version"`
	OccurredAt       time.Time              `json:"occurred_at"`
	LSN              string                 `json:"lsn"`
	Data             map[string]interface{} `json:"data"`
}

type Mapper struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Mapper {
	return &Mapper{cfg: cfg}
}

func (m *Mapper) Map(ce connector.ChangeEvent) (*AxyraEvent, error) {
	var tableCfg *config.TableConfig
	for i := range m.cfg.Tables {
		if m.cfg.Tables[i].Name == ce.Table {
			tableCfg = &m.cfg.Tables[i]
			break
		}
	}

	tenantCol := "store_id"
	pkCol := "id"
	if tableCfg != nil {
		if tableCfg.TenantIDColumn != "" {
			tenantCol = tableCfg.TenantIDColumn
		}
		if tableCfg.PKColumn != "" {
			pkCol = tableCfg.PKColumn
		}
	}

	// Extract external tenant ID
	extTenantID := ""
	if v, ok := ce.Row[tenantCol]; ok && v != nil {
		extTenantID = fmt.Sprintf("%v", v)
	}
	if extTenantID == "" {
		// Try fallback columns
		if v, ok := ce.Row["tenant_id"]; ok && v != nil {
			extTenantID = fmt.Sprintf("%v", v)
		} else {
			extTenantID = "default"
		}
	}

	// Extract primary key
	pkVal := ""
	if v, ok := ce.Row[pkCol]; ok && v != nil {
		pkVal = fmt.Sprintf("%v", v)
	} else if v, ok := ce.Row["order_id"]; ok && v != nil {
		pkVal = fmt.Sprintf("%v", v)
	} else if v, ok := ce.Row["id"]; ok && v != nil {
		pkVal = fmt.Sprintf("%v", v)
	}

	// Generate deterministic event ID derived from LSN + Table + PK
	hashSeed := fmt.Sprintf("%s:%s:%s", ce.LSN, ce.Table, pkVal)
	eventID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(hashSeed)).String()

	// Determine event type
	status := ""
	if v, ok := ce.Row["status"]; ok && v != nil {
		status = fmt.Sprintf("%v", v)
	}

	eventType := fmt.Sprintf("%s.%s", ce.Table, ce.Operation)
	if ce.Table == "orders" || ce.Table == "order" {
		if status == "cancelled" {
			eventType = "order.cancelled"
		} else if ce.Operation == "INSERT" {
			eventType = "order.created"
		} else if ce.Operation == "UPDATE" {
			eventType = "order.updated"
		} else if ce.Operation == "DELETE" {
			eventType = "order.deleted"
		}
	}

	occurredAt := ce.Timestamp
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	// Filter columns if tableCfg specifies columns
	data := make(map[string]interface{})
	if tableCfg != nil && len(tableCfg.Columns) > 0 {
		for _, col := range tableCfg.Columns {
			if val, ok := ce.Row[col]; ok {
				data[col] = val
			}
		}
	} else {
		data = ce.Row
	}

	// Ensure order_id is present in data
	if _, ok := data["order_id"]; !ok {
		if idVal, ok := ce.Row["id"]; ok {
			data["order_id"] = idVal
		}
	}

	return &AxyraEvent{
		EventID:          eventID,
		AgentID:          m.cfg.AgentID,
		ExternalTenantID: extTenantID,
		EventType:        eventType,
		SchemaVersion:    1,
		OccurredAt:       occurredAt,
		LSN:              ce.LSN,
		Data:             data,
	}, nil
}
