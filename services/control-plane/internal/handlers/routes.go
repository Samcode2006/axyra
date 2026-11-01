package handlers

import (
	"net/http"

	"github.com/axyra/control-plane/internal/middleware"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// Routes builds the control plane's HTTP API. Only /health and login are
// public; everything else requires a valid console session.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(middleware.CORS)

	r.Get("/health", h.Health)
	r.Post("/v1/auth/login", h.Login)
	r.With(middleware.ServiceAuth(h.cfg.ServiceToken)).Get("/internal/metric-definitions/{metric_id}", h.GetMetricForTenant)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Authenticate(h.jwtSecret))

		r.Get("/v1/auth/me", h.Me)

		r.Get("/v1/organizations", h.ListOrganizations)
		r.Post("/v1/organizations", h.CreateOrganization)
		r.Get("/v1/organizations/{id}", h.GetOrganization)
		r.Patch("/v1/organizations/{id}", h.UpdateOrganization)
		r.Delete("/v1/organizations/{id}", h.DeleteOrganization)
		r.Get("/v1/organizations/{id}/users", h.ListOrganizationUsers)
		r.Post("/v1/organizations/{id}/users", h.CreateOrganizationUser)
		r.Delete("/v1/organizations/{id}/users/{user_id}", h.DeleteOrganizationUser)

		r.Get("/v1/applications", h.ListApplications)
		r.Post("/v1/applications", h.CreateApplication)
		r.Get("/v1/applications/{id}", h.GetApplication)
		r.Patch("/v1/applications/{id}", h.UpdateApplication)
		r.Delete("/v1/applications/{id}", h.DeleteApplication)

		r.Get("/v1/agents", h.ListAgents)
		r.Post("/v1/agents", h.CreateAgent)
		r.Get("/v1/agents/{agent_id}", h.GetAgent)
		r.Patch("/v1/agents/{agent_id}", h.UpdateAgent)
		r.Delete("/v1/agents/{agent_id}", h.DeleteAgent)

		r.Get("/v1/agents/{agent_id}/tokens", h.ListAgentTokens)
		r.Post("/v1/agents/{agent_id}/tokens", h.IssueAgentToken)
		r.Post("/v1/agents/{agent_id}/tokens/{token_id}/rotate", h.RotateAgentToken)
		r.Delete("/v1/agents/{agent_id}/tokens/{token_id}", h.RevokeAgentToken)

		r.Get("/v1/agents/{agent_id}/tenant-mappings", h.ListTenantMappings)
		r.Post("/v1/agents/{agent_id}/tenant-mappings", h.CreateTenantMapping)
		r.Delete("/v1/agents/{agent_id}/tenant-mappings/{mapping_id}", h.DeleteTenantMapping)

		r.Get("/v1/tenants", h.ListTenants)
		r.Post("/v1/tenants", h.CreateTenant)
		r.Get("/v1/tenants/{id}", h.GetTenant)
		r.Get("/v1/tenants/{id}/provisioning", h.ProvisioningStatus)
		r.Post("/v1/tenants/{id}/provisioning/retry", h.RetryProvisioning)
		r.Patch("/v1/tenants/{id}", h.UpdateTenant)
		r.Delete("/v1/tenants/{id}", h.DeleteTenant)

		r.Get("/v1/policies", h.ListPolicies)
		r.Post("/v1/policies", h.CreatePolicy)
		r.Get("/v1/policies/{id}", h.GetPolicy)
		r.Patch("/v1/policies/{id}", h.UpdatePolicy)
		r.Delete("/v1/policies/{id}", h.DeletePolicy)

		r.Get("/v1/metric-definitions", h.ListMetricDefinitions)
		r.Get("/v1/metric-definitions/{metric_id}", h.GetMetricDefinition)
		r.Put("/v1/metric-definitions/{metric_id}", h.PutMetricDefinition)
		r.Delete("/v1/metric-definitions/{metric_id}", h.DeleteMetricDefinition)

		r.Get("/v1/audit-logs", h.ListAuditLogs)

		// Dashboard analytics (including dead-letter inspection) are proxied
		// so the web app needs only one origin.
		r.HandleFunc("/v1/analytics/*", h.AnalyticsProxy())
	})

	return r
}
