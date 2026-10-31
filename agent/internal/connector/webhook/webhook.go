package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/axyra/agent/internal/connector"
	"github.com/google/uuid"
)

type WebhookRequest struct {
	Table     string                 `json:"table"`
	Operation string                 `json:"operation"` // "INSERT", "UPDATE", "DELETE"
	Row       map[string]interface{} `json:"row"`
	OldRow    map[string]interface{} `json:"old_row,omitempty"`
}

type WebhookConnector struct {
	port   string
	server *http.Server
}

func New(port string) *WebhookConnector {
	if port == "" {
		port = "8084"
	}
	return &WebhookConnector{port: port}
}

func (w *WebhookConnector) Start(ctx context.Context, out chan<- connector.ChangeEvent) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(rw, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req WebhookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(rw, "Bad request", http.StatusBadRequest)
			return
		}

		if req.Table == "" || req.Row == nil {
			http.Error(rw, "Table and row are required", http.StatusBadRequest)
			return
		}

		op := req.Operation
		if op == "" {
			op = "INSERT"
		}

		lsn := fmt.Sprintf("wh/%s", uuid.New().String()[:8])
		change := connector.ChangeEvent{
			Table:     req.Table,
			Operation: op,
			LSN:       lsn,
			Timestamp: time.Now().UTC(),
			Row:       req.Row,
			OldRow:    req.OldRow,
		}

		out <- change

		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(rw).Encode(map[string]string{"status": "received", "lsn": lsn})
	})

	w.server = &http.Server{
		Addr:    ":" + w.port,
		Handler: mux,
	}

	log.Printf("[agent-webhook] webhook listener starting on port %s", w.port)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = w.server.Shutdown(shutdownCtx)
	}()

	if err := w.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (w *WebhookConnector) Close() error {
	if w.server != nil {
		return w.server.Close()
	}
	return nil
}

