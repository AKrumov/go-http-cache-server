// Package middleware provides HTTP middleware for the cache server.
package middleware

import (
	"context"
	"log/slog"
	"math/rand"
	"net/http"
	"sync"

	"go_http_cache_server/metrics"
	"golang.org/x/time/rate"
)

// RequestIDKey is the context key for request IDs.
type requestIDKey struct{}

// RequestID generates a short request ID and injects it into the context.
func RequestID(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = generateID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next(w, r.WithContext(ctx))
	}
}

// GetRequestID retrieves the request ID from the context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

const idAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateID() string {
	b := make([]byte, 12)
	for i := range b {
		b[i] = idAlphabet[rand.Intn(len(idAlphabet))]
	}
	return string(b)
}

// ------------------------------------------------------------------
// Rate limiter
// ------------------------------------------------------------------

// RateLimiter provides per-IP and global rate limiting.
type RateLimiter struct {
	global    *rate.Limiter
	perIP     map[string]*rate.Limiter
	mu        sync.RWMutex
	perIPRate rate.Limit
	perIPBurst int
}

// NewRateLimiter creates a rate limiter. If perIPRate is 0, per-IP limiting is disabled.
func NewRateLimiter(globalRate float64, perIPRate float64) *RateLimiter {
	limit := &RateLimiter{}
	if globalRate > 0 {
		limit.global = rate.NewLimiter(rate.Limit(globalRate), int(globalRate*2))
	}
	if perIPRate > 0 {
		limit.perIPRate = rate.Limit(perIPRate)
		limit.perIPBurst = int(perIPRate * 2)
		if limit.perIPBurst < 1 {
			limit.perIPBurst = 1
		}
		limit.perIP = make(map[string]*rate.Limiter)
	}
	return limit
}

// Allow reports whether the request from the given IP is allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	if rl.global != nil && !rl.global.Allow() {
		return false
	}
	if rl.perIPRate > 0 {
		l := rl.getLimiter(ip)
		if !l.Allow() {
			return false
		}
	}
	return true
}

func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.RLock()
	l, ok := rl.perIP[ip]
	rl.mu.RUnlock()
	if ok {
		return l
	}

	rl.mu.Lock()
	l, ok = rl.perIP[ip]
	if !ok {
		l = rate.NewLimiter(rl.perIPRate, rl.perIPBurst)
		rl.perIP[ip] = l
	}
	rl.mu.Unlock()
	return l
}

// RateLimit middleware rejects requests with 429 when rate limit is exceeded.
func RateLimit(rl *RateLimiter) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			if !rl.Allow(ip) {
				metrics.RateLimitHit()
				slog.Warn("rate limit exceeded", "ip", ip, "path", r.URL.Path)
				w.Header().Set("Retry-After", "1")
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
			next(w, r)
		}
	}
}
