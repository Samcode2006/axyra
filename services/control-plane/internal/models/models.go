package models

import (
	"encoding/json"
	"time"
)

type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateOrganizationRequest struct {
	Name string `json:"name"`
}

type Application struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Name           string    `json:"name"`
	Description    *string   `json:"description,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type CreateApplicationRequest struct {
	OrganizationID string  `json:"organization_id"`
	Name           string  `json:"name"`
	Description    *string `json:"description,omitempty"`
}

type Agent struct {
	ID            string     `json:"id"`
	ApplicationID string     `json:"application_id"`
	Name          string     `json:"name"`
	Status        string     `json:"status"` // 'connected' | 'disconnected' | 'error'
	LastSeenAt    *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type CreateAgentRequest struct {
	ApplicationID string `json:"application_id"`
	Name          string `json:"name"`
}

type AgentToken struct {
	ID        string     `json:"id"`
	AgentID   string     `json:"agent_id"`
	Token     string     `json:"token"`
	Name      *string    `json:"name,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type IssueTokenResponse struct {
	TokenID string `json:"token_id"`
	Token   string `json:"token"`
}

type TenantQuota struct {
	MaxEventsPerDay      int64 `json:"max_events_per_day"`
	MaxConcurrentQueries int   `json:"max_concurrent_queries"`
}

type Tenant struct {
	ID               string       `json:"id"`
	OrganizationID   string       `json:"organization_id"`
	ExternalTenantID string       `json:"external_tenant_id"`
	Name             string       `json:"name"`
	Status           string       `json:"status"` // 'active' | 'suspended' | 'inactive'
	Quota            *TenantQuota `json:"quota,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
}

type CreateTenantRequest struct {
	OrganizationID   string `json:"organization_id"`
	ExternalTenantID string `json:"external_tenant_id"`
	Name             string `json:"name"`
}

type UpdateTenantRequest struct {
	Status *string      `json:"status,omitempty"`
	Quota  *TenantQuota `json:"quota,omitempty"`
}

type TenantMapping struct {
	ID               string    `json:"id"`
	AgentID          string    `json:"agent_id"`
	ExternalTenantID string    `json:"external_tenant_id"`
	TenantID         string    `json:"tenant_id"`
	CreatedAt        time.Time `json:"created_at"`
}

type CreateTenantMappingRequest struct {
	ExternalTenantID string `json:"external_tenant_id"`
	TenantID         string `json:"tenant_id"`
}

type Policy struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenant_id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"` // 'rate_limit' | 'query_limit' | 'data_access'
	Config    json.RawMessage `json:"config"`
	CreatedAt time.Time       `json:"created_at"`
}

type CreatePolicyRequest struct {
	TenantID string          `json:"tenant_id"`
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Config   json.RawMessage `json:"config"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

type PaginatedResponse[T any] struct {
	Data    []T `json:"data"`
	Total   int `json:"total"`
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
}

