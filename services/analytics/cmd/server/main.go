package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/axyra/analytics/internal/config"
	"github.com/axyra/analytics/internal/db"
	"github.com/axyra/analytics/internal/handlers"
	"github.com/axyra/analytics/internal/middleware"
	"github.com/axyra/analytics/internal/queue"
	"github.com/axyra/analytics/internal/worker"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func main() {
	log.Println("[analytics] starting up...")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[analytics] config error: %v", err)
	}

	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[analytics] database connection failed: %v", err)
	}
	defer database.Close()

	q, err := queue.NewRedisQueue(cfg.RedisURL)
	if err != nil {
		log.Fatalf("[analytics] queue init failed: %v", err)
	}
	defer q.Close()

	// Start background worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := worker.New(database, q)
	w.Start(ctx)

	h := handlers.New(database, q, cfg)

	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(middleware.CORS)

	// Health check
	r.Get("/health", h.Health)

	// Agent ingestion endpoints
	r.Post("/v1/agent/events", h.IngestEvents)
	r.Get("/v1/agent/ack", h.AgentAck)

	// Tenant-scoped analytics endpoints
	r.Get("/v1/analytics/orders/summary", h.OrderSummary)
	r.Get("/v1/analytics/orders/trend", h.OrderTrend)
	r.Get("/v1/analytics/products/top", h.TopProducts)
	r.Get("/v1/analytics/orders/breakdown", h.OrderBreakdown)
	r.Get("/v1/analytics/status", h.Status)

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	go func() {
		log.Printf("[analytics] server listening on port %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[analytics] server exited: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("[analytics] shutting down...")

	w.Stop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	log.Println("[analytics] server stopped")
}

