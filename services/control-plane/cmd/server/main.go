package main

import (
	"log"
	"net/http"
	"time"

	"github.com/axyra/control-plane/internal/config"
	"github.com/axyra/control-plane/internal/db"
	"github.com/axyra/control-plane/internal/handlers"
)

func main() {
	log.Println("[control-plane] starting up...")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[control-plane] config error: %v", err)
	}
	for _, w := range cfg.Warnings {
		log.Printf("[control-plane] WARNING: %s", w)
	}

	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[control-plane] database connection failed: %v", err)
	}
	defer database.Close()

	if err := db.SeedDefaultData(database, db.SeedOptions{
		AgentToken:       cfg.DefaultAgentToken,
		TenantCount:      cfg.TenantCount,
		AdminUsername:    cfg.AdminUsername,
		AdminPassword:    cfg.AdminPassword,
		OperatorUsername: cfg.OperatorUsername,
		OperatorPassword: cfg.OperatorPassword,
	}); err != nil {
		log.Printf("[control-plane] warning: failed to seed default data: %v", err)
	}

	h := handlers.New(database, cfg)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("[control-plane] server listening on port %s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("[control-plane] server exited: %v", err)
	}
}
