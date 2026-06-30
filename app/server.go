package main

import (
	"time"

	"go_http_cache_server/health"
	"go_http_cache_server/middleware"
	"go_http_cache_server/storage"
)

type CacheServer struct {
	backend        storage.Backend
	maxUpload      int64
	auth           authConfig
	health         *health.Registry
	rateLimiter    *middleware.RateLimiter
	requestTimeout time.Duration
}

// NewCacheServer creates a new cache server with the given backend.
func NewCacheServer(backend storage.Backend, maxUpload int64) *CacheServer {
	return &CacheServer{backend: backend, maxUpload: maxUpload}
}

// NewCacheServerWithAuth creates a new cache server with optional HTTP authentication.
func NewCacheServerWithAuth(backend storage.Backend, maxUpload int64, auth authConfig) *CacheServer {
	return &CacheServer{backend: backend, maxUpload: maxUpload, auth: auth}
}
