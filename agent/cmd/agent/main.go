package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/axyra/agent/internal/config"
	"github.com/axyra/agent/internal/connector"
	"github.com/axyra/agent/internal/connector/postgres"
	"github.com/axyra/agent/internal/connector/webhook"
	"github.com/axyra/agent/internal/mapper"
	"github.com/axyra/agent/internal/sender"
)

func main() {
	configPath := flag.String("config", "/etc/axyra/agent.yaml", "Path to agent configuration YAML file")
	flag.Parse()

	log.Println("[axyra-agent] starting Axyra Agent binary...")

	// If default config path doesn't exist, check local current directory or fallback
	path := *configPath
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if _, err := os.Stat("agent.yaml"); err == nil {
			path = "agent.yaml"
		} else {
			path = "" // use environment variables & defaults
		}
	}

	cfg, err := config.Load(path)
	if err != nil {
		log.Fatalf("[axyra-agent] failed to load configuration: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Health check server
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","agent_id":"%s"}`, cfg.AgentID)
	})
	healthServer := &http.Server{
		Addr:    ":" + cfg.HealthPort,
		Handler: healthMux,
	}
	go func() {
		log.Printf("[axyra-agent] local health server running on port %s", cfg.HealthPort)
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[axyra-agent] health server error: %v", err)
		}
	}()

	// 2. Fetch confirmed LSN from the data plane ack endpoint (restart safety).
	// On first run this returns "0/0" and we start from the WAL tip.
	confirmedLSN := fetchConfirmedLSN(cfg)

	// 3. Initialize sender and mapper
	snd := sender.New(cfg)
	snd.Start(ctx)

	m := mapper.New(cfg)

	changeEvents := make(chan connector.ChangeEvent, 500)

	// Pipe changeEvents -> mapper -> sender
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ce, ok := <-changeEvents:
				if !ok {
					return
				}
				evt, err := m.Map(ce)
				if err != nil {
					log.Printf("[axyra-agent] mapping error: %v", err)
					continue
				}
				snd.In() <- *evt
			}
		}
	}()

	// 4. Select and start connector
	var conn connector.Connector
	mode := os.Getenv("CONNECTOR_MODE")
	if mode == "webhook" {
		log.Println("[axyra-agent] using Webhook fallback connector")
		conn = webhook.New("8084")
	} else {
		log.Println("[axyra-agent] using PostgreSQL WAL logical replication connector")
		tableNames := make([]string, len(cfg.Tables))
		for i, t := range cfg.Tables {
			tableNames[i] = t.Name
		}
		pgConn := postgres.New(cfg.PostgresDSN, cfg.ReplicationSlot, cfg.PublicationName, tableNames)
		pgConn.WithStartLSN(confirmedLSN)
		conn = pgConn
	}

	go func() {
		if err := conn.Start(ctx, changeEvents); err != nil && err != context.Canceled {
			log.Printf("[axyra-agent] connector exited with error: %v", err)
		}
	}()

	// 5. Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[axyra-agent] shutting down...")
	cancel()
	_ = conn.Close()
	_ = snd.Close()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	_ = healthServer.Shutdown(shutdownCtx)

	log.Println("[axyra-agent] shutdown complete")
}

// fetchConfirmedLSN polls the analytics data plane ack endpoint and returns the
// last confirmed LSN for this agent. On any failure it returns "" so the
// connector starts from the current WAL tip (safe: idempotency handles re-delivery).
func fetchConfirmedLSN(cfg *config.Config) string {
	url := fmt.Sprintf("%s/v1/agent/ack", cfg.AxyraEndpoint)
	client := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("[axyra-agent] could not build ack request: %v", err)
		return ""
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.AgentToken))

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[axyra-agent] ack endpoint unreachable, starting from WAL tip: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[axyra-agent] ack endpoint returned %d, starting from WAL tip", resp.StatusCode)
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var ack struct {
		ConfirmedLSN string `json:"confirmed_lsn"`
	}
	if err := json.Unmarshal(body, &ack); err != nil {
		return ""
	}

	if ack.ConfirmedLSN != "" && ack.ConfirmedLSN != "0/0" {
		log.Printf("[axyra-agent] resuming from confirmed LSN: %s", ack.ConfirmedLSN)
	}
	return ack.ConfirmedLSN
}
