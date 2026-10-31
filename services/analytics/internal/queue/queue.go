package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/axyra/analytics/internal/models"
	"github.com/redis/go-redis/v9"
)

const DefaultQueueKey = "axyra:events:queue"

type Queue interface {
	Push(ctx context.Context, events []models.EventEnvelope) error
	Pop(ctx context.Context, timeout time.Duration) (*models.EventEnvelope, error)
	Close() error
}

type RedisQueue struct {
	client   *redis.Client
	queueKey string
}

func NewRedisQueue(redisURL string) (Queue, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	client := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("[analytics-queue] warning: redis ping failed: %v. Using in-memory fallback queue.", err)
		return NewMemoryQueue(10000), nil
	}

	log.Println("[analytics-queue] connected to redis")
	return &RedisQueue{
		client:   client,
		queueKey: DefaultQueueKey,
	}, nil
}

func (q *RedisQueue) Push(ctx context.Context, events []models.EventEnvelope) error {
	if len(events) == 0 {
		return nil
	}
	values := make([]interface{}, len(events))
	for i, e := range events {
		bytes, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
		values[i] = bytes
	}
	return q.client.RPush(ctx, q.queueKey, values...).Err()
}

func (q *RedisQueue) Pop(ctx context.Context, timeout time.Duration) (*models.EventEnvelope, error) {
	res, err := q.client.BLPop(ctx, timeout, q.queueKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // timeout, no items
		}
		return nil, err
	}
	if len(res) < 2 {
		return nil, nil
	}

	var event models.EventEnvelope
	if err := json.Unmarshal([]byte(res[1]), &event); err != nil {
		return nil, fmt.Errorf("unmarshal event: %w", err)
	}
	return &event, nil
}

func (q *RedisQueue) Close() error {
	return q.client.Close()
}

// Depth returns the number of items currently waiting in the queue.
func (q *RedisQueue) Depth(ctx context.Context) (int64, error) {
	return q.client.LLen(ctx, q.queueKey).Result()
}

// MemoryQueue provides in-memory fallback for local testing
type MemoryQueue struct {
	ch   chan models.EventEnvelope
	mu   sync.Mutex
	done chan struct{}
}

func NewMemoryQueue(bufferSize int) Queue {
	return &MemoryQueue{
		ch:   make(chan models.EventEnvelope, bufferSize),
		done: make(chan struct{}),
	}
}

func (m *MemoryQueue) Push(ctx context.Context, events []models.EventEnvelope) error {
	for _, e := range events {
		select {
		case m.ch <- e:
		case <-ctx.Done():
			return ctx.Err()
		case <-m.done:
			return fmt.Errorf("queue closed")
		}
	}
	return nil
}

func (m *MemoryQueue) Pop(ctx context.Context, timeout time.Duration) (*models.EventEnvelope, error) {
	select {
	case e := <-m.ch:
		return &e, nil
	case <-time.After(timeout):
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.done:
		return nil, nil
	}
}

func (m *MemoryQueue) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case <-m.done:
	default:
		close(m.done)
	}
	return nil
}

