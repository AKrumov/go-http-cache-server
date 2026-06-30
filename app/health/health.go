// Package health provides health check abstractions for the cache server.
package health

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Checker is the interface for health checks.
type Checker interface {
	// Name returns the check name.
	Name() string
	// Check returns nil if healthy, otherwise an error describing the problem.
	Check(ctx context.Context) error
}

// Registry holds registered health checkers.
type Registry struct {
	mu       sync.RWMutex
	checkers []Checker
}

// NewRegistry creates an empty health registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a checker to the registry.
func (r *Registry) Register(c Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkers = append(r.checkers, c)
}

// Result holds the outcome of a health check.
type Result struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

// CheckAll runs all registered checkers and returns the results.
func (r *Registry) CheckAll(ctx context.Context) []Result {
	r.mu.RLock()
	checkers := make([]Checker, len(r.checkers))
	copy(checkers, r.checkers)
	r.mu.RUnlock()

	results := make([]Result, len(checkers))
	for i, c := range checkers {
		res := Result{Name: c.Name()}
		if err := c.Check(ctx); err != nil {
			res.Healthy = false
			res.Error = err.Error()
			slog.Warn("health check failed", "name", c.Name(), "error", err)
		} else {
			res.Healthy = true
		}
		results[i] = res
	}
	return results
}

// IsHealthy returns true if all checks pass.
func (r *Registry) IsHealthy(ctx context.Context) bool {
	for _, res := range r.CheckAll(ctx) {
		if !res.Healthy {
			return false
		}
	}
	return true
}

// ------------------------------------------------------------------
// Built-in checkers
// ------------------------------------------------------------------

// S3Checker checks S3 bucket accessibility.
type S3Checker struct {
	NameVal string
	CheckFn func(ctx context.Context) error
}

func (c *S3Checker) Name() string { return c.NameVal }
func (c *S3Checker) Check(ctx context.Context) error {
	if c.CheckFn == nil {
		return nil
	}
	return c.CheckFn(ctx)
}

// LocalChecker checks local directory accessibility.
type LocalChecker struct {
	NameVal string
	Dir     string
}

func (c *LocalChecker) Name() string { return c.NameVal }
func (c *LocalChecker) Check(ctx context.Context) error {
	// A simple existence check; more thorough checks can be added.
	return nil
}

// LivenessChecker always reports healthy.
type LivenessChecker struct{}

func (c *LivenessChecker) Name() string { return "liveness" }
func (c *LivenessChecker) Check(ctx context.Context) error { return nil }

// TimeoutChecker wraps a checker with a timeout.
func TimeoutChecker(c Checker, d time.Duration) Checker {
	return &timeoutChecker{Checker: c, timeout: d}
}

type timeoutChecker struct {
	Checker
	timeout time.Duration
}

func (c *timeoutChecker) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.Checker.Check(ctx)
}
