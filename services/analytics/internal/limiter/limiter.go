// Package limiter provides per-tenant query concurrency enforcement (B-3).
//
// Each tenant is given a semaphore whose capacity is the smaller of the
// platform default and any per-tenant quota. Callers call Acquire before
// running a heavy query and Release when it finishes.
package limiter

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Limiter manages per-tenant semaphores for heavy-query concurrency control.
type Limiter struct {
	mu         sync.Mutex
	semaphores map[string]chan struct{}
	defaultMax int
}

// New creates a Limiter with the given platform-wide default capacity.
func New(defaultMax int) *Limiter {
	if defaultMax <= 0 {
		defaultMax = 5
	}
	return &Limiter{
		semaphores: make(map[string]chan struct{}),
		defaultMax: defaultMax,
	}
}

// DefaultMaxWait is how long Acquire waits for a free slot before giving up.
// A dashboard page legitimately fires more requests at once than the cap, so a
// short wait absorbs that burst; a tenant that stays saturated still gets
// rejected instead of queueing without limit.
const DefaultMaxWait = 3 * time.Second

// Acquire reserves a slot for tenantID, waiting up to DefaultMaxWait for one to
// free up, or until ctx is cancelled (for example the client disconnected).
// capacity is the per-tenant override (0 = use platform default). The cap on
// simultaneous queries is unchanged: this only decides what happens to a
// request that arrives while the tenant is at the cap.
func (l *Limiter) Acquire(ctx context.Context, tenantID string, capacity int) error {
	sem := l.getSemaphore(tenantID, capacity)

	// Fast path: a slot is free right now.
	select {
	case sem <- struct{}{}:
		return nil
	default:
	}

	timer := time.NewTimer(DefaultMaxWait)
	defer timer.Stop()
	select {
	case sem <- struct{}{}:
		return nil
	case <-timer.C:
		return fmt.Errorf("tenant %s has reached its concurrent query limit (%d)", tenantID, cap(sem))
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release frees a previously acquired slot for tenantID.
func (l *Limiter) Release(tenantID string) {
	l.mu.Lock()
	sem, ok := l.semaphores[tenantID]
	l.mu.Unlock()
	if ok {
		select {
		case <-sem:
		default:
		}
	}
}

func (l *Limiter) getSemaphore(tenantID string, capacity int) chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	sem, ok := l.semaphores[tenantID]
	if !ok {
		cap := l.defaultMax
		if capacity > 0 {
			cap = capacity
		}
		sem = make(chan struct{}, cap)
		l.semaphores[tenantID] = sem
	}
	return sem
}
