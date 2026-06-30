package storage

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// CircuitBreaker wraps a Backend with a simple circuit breaker.
// After a threshold of consecutive failures, the breaker opens and
// rejects all requests for a cooldown period.
type CircuitBreaker struct {
	backend   Backend
	threshold int
	timeout   time.Duration

	mu        sync.RWMutex
	failures  int
	lastFail  time.Time
	open      bool
}

// NewCircuitBreaker creates a circuit breaker.
// threshold: consecutive failures before opening.
// timeout: duration the breaker stays open before allowing a probe.
func NewCircuitBreaker(b Backend, threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		backend:   b,
		threshold: threshold,
		timeout:   timeout,
	}
}

// State returns the current breaker state: "closed", "open", or "half-open".
func (cb *CircuitBreaker) State() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	if cb.open {
		if time.Since(cb.lastFail) > cb.timeout {
			return "half-open"
		}
		return "open"
	}
	return "closed"
}

func (cb *CircuitBreaker) allow() bool {
	cb.mu.RLock()
	open := cb.open
	lastFail := cb.lastFail
	cb.mu.RUnlock()

	if !open {
		return true
	}
	if time.Since(lastFail) > cb.timeout {
		return true
	}
	return false
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	cb.failures = 0
	cb.open = false
	cb.mu.Unlock()
}

func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	cb.failures++
	cb.lastFail = time.Now()
	if cb.failures >= cb.threshold {
		cb.open = true
	}
	cb.mu.Unlock()
}

// Head implements Backend.
func (cb *CircuitBreaker) Head(ctx context.Context, key string) (int64, bool, error) {
	if !cb.allow() {
		return 0, false, fmt.Errorf("circuit breaker open")
	}
	size, exists, err := cb.backend.Head(ctx, key)
	if err != nil {
		cb.recordFailure()
		return 0, false, err
	}
	cb.recordSuccess()
	return size, exists, nil
}

// Get implements Backend.
func (cb *CircuitBreaker) Get(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
	if !cb.allow() {
		return nil, 0, time.Time{}, false, fmt.Errorf("circuit breaker open")
	}
	rc, size, modTime, exists, err := cb.backend.Get(ctx, key)
	if err != nil {
		cb.recordFailure()
		return nil, 0, time.Time{}, false, err
	}
	// For Get, we don't record success until the stream is fully read.
	// We wrap the ReadCloser to detect errors on close.
	return &cbReadCloser{ReadCloser: rc, cb: cb}, size, modTime, exists, nil
}

// Put implements Backend.
func (cb *CircuitBreaker) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if !cb.allow() {
		return fmt.Errorf("circuit breaker open")
	}
	if err := cb.backend.Put(ctx, key, r, size); err != nil {
		cb.recordFailure()
		return err
	}
	cb.recordSuccess()
	return nil
}

// Delete implements Backend.
func (cb *CircuitBreaker) Delete(ctx context.Context, key string) error {
	if !cb.allow() {
		return fmt.Errorf("circuit breaker open")
	}
	if err := cb.backend.Delete(ctx, key); err != nil {
		cb.recordFailure()
		return err
	}
	cb.recordSuccess()
	return nil
}

type cbReadCloser struct {
	io.ReadCloser
	cb *CircuitBreaker
}

func (r *cbReadCloser) Close() error {
	err := r.ReadCloser.Close()
	if err != nil {
		r.cb.recordFailure()
	} else {
		r.cb.recordSuccess()
	}
	return err
}

var _ Backend = (*CircuitBreaker)(nil)
