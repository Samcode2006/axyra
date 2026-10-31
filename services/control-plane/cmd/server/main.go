package main

import (
	"log"
	"net/http"

	"github.com/axyra/control-plane/internal/config"
	"github.com/axyra/control-plane/internal/db"
	"github.com/axyra/control-plane/internal/handlers"
	"github.com/axyra/control-plane/internal/middleware"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func main() {
	log.Println("[control-plane] starting up...")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[control-plane] config error: %v", err)
	}

	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[control-plane] database connection failed: %v", err)
	}
	defer database.Close()

	if err := db.SeedDefaultData(database, cfg.DefaultAgentToken); err != nil {
		log.Printf("[control-plane] warning: failed to seed default data: %v", err)
	}

	h := handlers.New(database, cfg)

	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(middleware.CORS)

	// Health check
	r.Get("/health", h.Health)

	// Auth
	r.Post("/v1/auth/login", h.Login)

	// Organizations
	r.Get("/v1/organizations", h.ListOrganizations)
	r.Post("/v1/organizations", h.CreateOrganization)

	// Applications
	r.Get("/v1/applications", h.ListApplications)
	r.Post("/v1/applications", h.CreateApplication)

	// Agents & Tokens
	r.Get("/v1/agents", h.ListAgents)
	r.Post("/v1/agents", h.CreateAgent)
	r.Post("/v1/agents/{agent_id}/tokens", h.IssueAgentToken)
	r.Delete("/v1/agents/{agent_id}/tokens/{token_id}", h.RevokeAgentToken)

	// Tenant Mappings
	r.Get("/v1/agents/{agent_id}/tenant-mappings", h.ListTenantMappings)
	r.Post("/v1/agents/{agent_id}/tenant-mappings", h.CreateTenantMapping)

	// Tenants
	r.Get("/v1/tenants", h.ListTenants)
	r.Post("/v1/tenants", h.CreateTenant)
	r.Patch("/v1/tenants/{id}", h.UpdateTenant)

	// Policies
	r.Get("/v1/policies", h.ListPolicies)
	r.Post("/v1/policies", h.CreatePolicy)
	r.Delete("/v1/policies/{id}", h.DeletePolicy)

	// Reverse-proxy analytics routes so the React frontend can talk to control-plane port directly
	r.HandleFunc("/v1/analytics/*", h.AnalyticsProxy())

	log.Printf("[control-plane] server listening on port %s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("[control-plane] server exited: %v", err)
	}
}

