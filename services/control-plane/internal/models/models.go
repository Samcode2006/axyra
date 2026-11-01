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

type UpdateOrganizationRequest struct {
	Name *string `json:"name,omitempty"`
}

type Application struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Name           string    `json:"name"`
	Description    *string   `json:"description,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type CreateApplicationRequest struct {
	// OrganizationID is optional for customer admins (it defaults to their own
	// organization) and required for platform operators.
	OrganizationID string  `json:"organization_id"`
	Name           string  `json:"name"`
	Description    *string `json:"description,omitempty"`
}

type UpdateApplicationRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
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

type UpdateAgentRequest struct {
	Name *string `json:"name,omitempty"`
}

// AgentToken is the listing view of a credential. The secret itself is never
// stored or returned after issuance; TokenPrefix identifies it in the console.
type AgentToken struct {
	ID          string     `json:"id"`
	AgentID     string     `json:"agent_id"`
	Name        *string    `json:"name,omitempty"`
	TokenPrefix string     `json:"token_prefix"`
	Status      string     `json:"status"` // 'active' | 'expired' | 'revoked'
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

type IssueTokenRequest struct {
	Name          *string `json:"name,omitempty"`
	ExpiresInDays *int    `json:"expires_in_days,omitempty"`
}

type IssueTokenResponse struct {
	TokenID     string     `json:"token_id"`
	Token       string     `json:"token"` // shown exactly once
	TokenPrefix string     `json:"token_prefix"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type RotateTokenRequest struct {
	// GracePeriodSeconds keeps the old token valid for a short overlap so a
	// running agent can be restarted with the new one. 0 revokes it immediately.
	GracePeriodSeconds int  `json:"grace_period_seconds"`
	ExpiresInDays      *int `json:"expires_in_days,omitempty"`
}

type RotateTokenResponse struct {
	IssueTokenResponse
	PreviousTokenID        string     `json:"previous_token_id"`
	PreviousTokenExpiresAt *time.Time `json:"previous_token_expires_at,omitempty"`
}

type TenantQuota struct {
	MaxEventsPerDay      int64 `json:"max_events_per_day"`
	MaxConcurrentQueries int   `json:"max_concurrent_queries"`
}

// UpdateTenantQuota allows changing one limit without restating the other.
type UpdateTenantQuota struct {
	MaxEventsPerDay      *int64 `json:"max_events_per_day,omitempty"`
	MaxConcurrentQueries *int   `json:"max_concurrent_queries,omitempty"`
}

type Tenant struct {
	ID               string          `json:"id"`
	OrganizationID   string          `json:"organization_id"`
	ExternalTenantID string          `json:"external_tenant_id"`
	Name             string          `json:"name"`
	Status           string          `json:"status"` // 'active' | 'suspended' | 'inactive'
	Quota            *TenantQuota    `json:"quota,omitempty"`
	Region           string          `json:"region"`        // 'apac' | 'emea' | 'americas' | 'global'
	PlanTier         string          `json:"plan_tier"`     // 'free' | 'growth' | 'enterprise'
	BillingCycle     string          `json:"billing_cycle"` // 'monthly' | 'annual'
	Tags             []string        `json:"tags"`
	Metadata         json.RawMessage `json:"metadata"`
	CreatedAt        time.Time       `json:"created_at"`
}

type CreateTenantRequest struct {
	DatabaseMode     string             `json:"database_mode,omitempty"`
	OrganizationID   string             `json:"organization_id"`
	ExternalTenantID string             `json:"external_tenant_id"`
	Name             string             `json:"name"`
	Quota            *UpdateTenantQuota `json:"quota,omitempty"`
	Region           *string            `json:"region,omitempty"`
	PlanTier         *string            `json:"plan_tier,omitempty"`
	BillingCycle     *string            `json:"billing_cycle,omitempty"`
	Tags             []string           `json:"tags,omitempty"`
	Metadata         json.RawMessage    `json:"metadata,omitempty"`
}

type UpdateTenantRequest struct {
	Name         *string            `json:"name,omitempty"`
	Status       *string            `json:"status,omitempty"`
	Quota        *UpdateTenantQuota `json:"quota,omitempty"`
	Region       *string            `json:"region,omitempty"`
	PlanTier     *string            `json:"plan_tier,omitempty"`
	BillingCycle *string            `json:"billing_cycle,omitempty"`
	// Tags replaces the whole list when present (an empty list clears it).
	Tags     *[]string       `json:"tags,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
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

type UpdatePolicyRequest struct {
	Name   *string         `json:"name,omitempty"`
	Config json.RawMessage `json:"config,omitempty"`
}

type MetricDefinition struct {
	ID             string          `json:"id"`
	OrganizationID *string         `json:"organization_id"` // null for built-in definitions
	Name           string          `json:"name"`
	Description    *string         `json:"description,omitempty"`
	Config         json.RawMessage `json:"config"`
	Enabled        bool            `json:"enabled"`
	Builtin        bool            `json:"builtin"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type PutMetricDefinitionRequest struct {
	// OrganizationID selects the owner. Admins may only use their own
	// organization; operators may omit it to edit a built-in definition.
	OrganizationID string          `json:"organization_id"`
	Name           string          `json:"name"`
	Description    *string         `json:"description,omitempty"`
	Config         json.RawMessage `json:"config"`
	Enabled        *bool           `json:"enabled,omitempty"`
}

type AuditLog struct {
	ID             int64           `json:"id"`
	OrganizationID *string         `json:"organization_id,omitempty"`
	ActorID        string          `json:"actor_id"`
	ActorName      string          `json:"actor_name"`
	ActorRole      string          `json:"actor_role"`
	Action         string          `json:"action"`
	ResourceType   string          `json:"resource_type"`
	ResourceID     string          `json:"resource_id"`
	Details        json.RawMessage `json:"details"`
	CreatedAt      time.Time       `json:"created_at"`
}

type AuditLogPage struct {
	Data []AuditLog `json:"data"`
	// NextBeforeID is the cursor for the next (older) page; absent on the last page.
	NextBeforeID *int64 `json:"next_before_id,omitempty"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type User struct {
	ID             string  `json:"id"`
	Username       string  `json:"username"`
	Role           string  `json:"role"` // 'admin' | 'operator'
	OrganizationID *string `json:"organization_id,omitempty"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type PaginatedResponse[T any] struct {
	Data    []T `json:"data"`
	Total   int `json:"total"`
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
}
