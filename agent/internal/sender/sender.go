package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/axyra/agent/internal/config"
	"github.com/axyra/agent/internal/mapper"
)

type Sender struct {
	cfg        *config.Config
	client     *http.Client
	in         chan mapper.AxyraEvent
	buffer     []mapper.AxyraEvent
	mu         sync.Mutex
	stop       chan struct{}
	done       chan struct{}
	retrySleep time.Duration
}

func New(cfg *config.Config) *Sender {
	return &Sender{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		in:         make(chan mapper.AxyraEvent, 1000),
		buffer:     make([]mapper.AxyraEvent, 0, cfg.BatchSize),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		retrySleep: 1 * time.Second,
	}
}

func (s *Sender) In() chan<- mapper.AxyraEvent {
	return s.in
}

func (s *Sender) Start(ctx context.Context) {
	go s.heartbeat(ctx)
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.cfg.FlushInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				s.flushAll()
				return
			case <-s.stop:
				s.flushAll()
				return
			case event, ok := <-s.in:
				if !ok {
					s.flushAll()
					return
				}
				s.mu.Lock()
				s.buffer = append(s.buffer, event)
				shouldFlush := len(s.buffer) >= s.cfg.BatchSize
				s.mu.Unlock()

				if shouldFlush {
					s.flush()
				}
			case <-ticker.C:
				s.flush()
			}
		}
	}()
}

func (s *Sender) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.AxyraEndpoint+"/v1/agent/heartbeat", nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+s.cfg.AgentToken)
			resp, err := s.client.Do(req)
			if err == nil {
				resp.Body.Close()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
		}
	}
}

func (s *Sender) flush() {
	s.mu.Lock()
	if len(s.buffer) == 0 {
		s.mu.Unlock()
		return
	}
	batch := make([]mapper.AxyraEvent, len(s.buffer))
	copy(batch, s.buffer)
	s.buffer = s.buffer[:0]
	s.mu.Unlock()

	s.sendBatchWithRetry(batch)
}

func (s *Sender) flushAll() {
	s.mu.Lock()
	if len(s.buffer) == 0 {
		s.mu.Unlock()
		return
	}
	batch := make([]mapper.AxyraEvent, len(s.buffer))
	copy(batch, s.buffer)
	s.buffer = s.buffer[:0]
	s.mu.Unlock()

	s.sendBatchWithRetry(batch)
}

func (s *Sender) sendBatchWithRetry(batch []mapper.AxyraEvent) {
	if len(batch) == 0 {
		return
	}

	payload := map[string]interface{}{
		"events": batch,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[agent-sender] marshal batch error: %v", err)
		return
	}

	url := fmt.Sprintf("%s/v1/agent/events", s.cfg.AxyraEndpoint)

	for attempt := 1; attempt <= 10; attempt++ {
		req, err := http.NewRequest("POST", url, bytes.NewReader(data))
		if err != nil {
			log.Printf("[agent-sender] create request error: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.cfg.AgentToken))

		resp, err := s.client.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
				_ = resp.Body.Close()
				log.Printf("[agent-sender] successfully streamed %d events to Axyra data plane (LSN: %s)", len(batch), batch[len(batch)-1].LSN)
				return
			}
			_ = resp.Body.Close()
			log.Printf("[agent-sender] server returned status %d on attempt %d", resp.StatusCode, attempt)
		} else {
			log.Printf("[agent-sender] send error on attempt %d: %v", attempt, err)
		}

		time.Sleep(s.retrySleep)
	}

	log.Printf("[agent-sender] ERROR: failed to deliver batch of %d events after 10 retries", len(batch))
}

func (s *Sender) Close() error {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	<-s.done
	return nil
}
