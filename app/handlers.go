package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"go_http_cache_server/metrics"
	"go_http_cache_server/middleware"
	"go_http_cache_server/storage"
)

func (cs *CacheServer) handleCache(w http.ResponseWriter, r *http.Request) {
	cacheID, entryKey, err := parseCachePath(r.URL.Path)
	if err != nil {
		slog.Warn("invalid cache key", "path", r.URL.Path, "request_id", middleware.GetRequestID(r.Context()))
		http.Error(w, "invalid cache key", http.StatusBadRequest)
		return
	}

	storageKey := makeStorageKey(cacheID, entryKey)
	ctx, cancel := cs.withRequestTimeout(r.Context())
	defer cancel()
	if rng := r.Header.Get("Range"); rng != "" {
		ctx = storage.WithRange(ctx, rng)
	}
	r = r.WithContext(ctx)

	switch r.Method {
	case http.MethodHead:
		cs.handleHead(w, r, storageKey, cacheID)
	case http.MethodGet:
		cs.handleGet(w, r, storageKey, cacheID)
	case http.MethodPut:
		cs.handlePut(w, r, storageKey, cacheID, cs.maxUpload)
	case http.MethodDelete:
		cs.handleDelete(w, r, storageKey, cacheID)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (cs *CacheServer) withRequestTimeout(parent context.Context) (context.Context, func()) {
	if cs.requestTimeout > 0 {
		ctx, cancel := context.WithTimeout(parent, cs.requestTimeout)
		return ctx, cancel
	}
	return parent, func() {}
}

func (cs *CacheServer) handleHead(w http.ResponseWriter, r *http.Request, storageKey string, cacheID string) {
	reqID := middleware.GetRequestID(r.Context())
	size, exists, err := cs.backend.Head(r.Context(), storageKey)
	if err != nil {
		slog.Error("error checking cache entry", "key", storageKey, "error", err, "request_id", reqID)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !exists {
		metrics.CacheMiss(safeCacheID(cacheID))
		http.NotFound(w, r)
		return
	}

	metrics.CacheHit(safeCacheID(cacheID))
	w.Header().Set("Content-Length", fmt.Sprint(size))
	w.WriteHeader(http.StatusOK)
}

func (cs *CacheServer) handleGet(w http.ResponseWriter, r *http.Request, storageKey string, cacheID string) {
	reqID := middleware.GetRequestID(r.Context())
	file, size, modTime, exists, err := cs.backend.Get(r.Context(), storageKey)
	if err != nil {
		slog.Error("error loading cache entry", "key", storageKey, "error", err, "request_id", reqID)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !exists {
		metrics.CacheMiss(safeCacheID(cacheID))
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	safeID := safeCacheID(cacheID)
	metrics.CacheHit(safeID)
	metrics.CacheServedBytes(safeID, size)

	if seeker, ok := file.(io.ReadSeeker); ok {
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, filepath.Base(storageKey), modTime, seeker)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprint(size))
		if !modTime.IsZero() {
			w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
		}
		_, copyErr := storage.PooledCopy(w, file)
		if copyErr != nil {
			slog.Warn("error serving cache entry", "key", storageKey, "error", copyErr, "request_id", reqID)
		}
	}
}

func (cs *CacheServer) handlePut(w http.ResponseWriter, r *http.Request, storageKey string, cacheID string, maxSize int64) {
	reqID := middleware.GetRequestID(r.Context())
	if r.ContentLength < 0 {
		http.Error(w, "Content-Length required", http.StatusLengthRequired)
		return
	}
	if maxSize > 0 && r.ContentLength > maxSize {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	if r.ContentLength == 0 {
		http.Error(w, "empty payload not allowed", http.StatusBadRequest)
		return
	}

	body := r.Body
	if maxSize > 0 {
		body = http.MaxBytesReader(w, r.Body, maxSize)
	}
	defer body.Close()

	err := cs.backend.Put(r.Context(), storageKey, body, r.ContentLength)
	if err != nil {
		slog.Error("error storing cache entry", "key", storageKey, "error", err, "request_id", reqID)
		http.Error(w, "failed to store cache entry", http.StatusInternalServerError)
		return
	}

	safeID := safeCacheID(cacheID)
	metrics.CacheEntryStored(safeID)
	metrics.CacheStoredBytes(safeID, r.ContentLength)

	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusCreated)
}

func (cs *CacheServer) handleDelete(w http.ResponseWriter, r *http.Request, storageKey string, cacheID string) {
	reqID := middleware.GetRequestID(r.Context())
	if err := cs.backend.Delete(r.Context(), storageKey); err != nil {
		slog.Error("error deleting cache entry", "key", storageKey, "error", err, "request_id", reqID)
		http.Error(w, "failed to delete cache entry", http.StatusInternalServerError)
		return
	}

	metrics.CacheEntryDeleted(safeCacheID(cacheID))
	w.WriteHeader(http.StatusNoContent)
}

func (cs *CacheServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (cs *CacheServer) handleLivez(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (cs *CacheServer) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if cs.health == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	results := cs.health.CheckAll(r.Context())
	allHealthy := true
	for _, res := range results {
		if !res.Healthy {
			allHealthy = false
			break
		}
	}
	if allHealthy {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(results)
	}
}

func (cs *CacheServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	info := map[string]string{
		"version": version,
	}
	_ = json.NewEncoder(w).Encode(info)
}

func (cs *CacheServer) instrument(handlerName string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		middleware.RequestID(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}
			metrics.InFlightInc()
			defer metrics.InFlightDec()
			start := time.Now()

			next(sw, r)

			cacheID := ""
			if id, ok := cacheIDFromPath(r.URL.Path); ok {
				cacheID = safeCacheID(id)
			}

			statusLabel := fmt.Sprint(sw.status)
			duration := time.Since(start)
			metrics.ObserveRequest(r.Method, handlerName, statusLabel, cacheID, duration)

			reqID := middleware.GetRequestID(r.Context())
			slog.Info("request handled",
				"handler", handlerName,
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration", duration,
				"cache_id", cacheID,
				"request_id", reqID,
			)
		})(w, r)
	}
}

func cacheIDFromPath(urlPath string) (string, bool) {
	cacheID, _, err := parseCachePath(urlPath)
	return cacheID, err == nil
}
