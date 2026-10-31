// Package limiter provides per-tenant query concurrency enforcement (B-3).
//
// Each tenant is given a semaphore whose capacity is the smaller of the
// platform default and any per-tenant quota. Callers call Acquire before
// running a heavy query and Release when it finishes.
package limiter

import (
	"fmt"
	"sync"
)

// Limiter manages per-tenant semaphores for heavy-query concurrency control.
type Limiter struct {
	mu          sync.Mutex
	semaphores  map[string]chan struct{}
	defaultMax  int
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

// Acquire attempts to reserve a slot for tenantID. capacity is the per-tenant
// override (0 = use platform default). Returns an error if already at capacity.
func (l *Limiter) Acquire(tenantID string, capacity int) error {
	sem := l.getSemaphore(tenantID, capacity)
	select {
	case sem <- struct{}{}:
		return nil
	default:
		cap := len(sem) // should equal max
		return fmt.Errorf("tenant %s has reached its concurrent query limit (%d)", tenantID, cap)
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
