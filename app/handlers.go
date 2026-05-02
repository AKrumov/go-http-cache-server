package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"go_gradle_cache/metrics"
)

func (cs *CacheServer) handleCache(w http.ResponseWriter, r *http.Request) {
	cacheID, entryKey, err := parseCachePath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid cache key", http.StatusBadRequest)
		return
	}

	storageKey := makeStorageKey(cacheID, entryKey)

	switch r.Method {
	case http.MethodHead:
		cs.handleHead(w, r, storageKey, cacheID)
	case http.MethodGet:
		cs.handleGet(w, r, storageKey, cacheID)
	case http.MethodPut:
		cs.handlePut(w, r, storageKey, cacheID, cs.maxUpload)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (cs *CacheServer) handleHead(w http.ResponseWriter, r *http.Request, storageKey string, cacheID string) {
	size, exists, err := cs.backend.Head(r.Context(), storageKey)
	if err != nil {
		log.Printf("error checking cache entry %s: %v", storageKey, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !exists {
		metrics.CacheMiss(cacheID)
		http.NotFound(w, r)
		return
	}

	metrics.CacheHit(cacheID)
	w.Header().Set("Content-Length", fmt.Sprint(size))
	w.WriteHeader(http.StatusOK)
}

func (cs *CacheServer) handleGet(w http.ResponseWriter, r *http.Request, storageKey string, cacheID string) {
	file, size, modTime, exists, err := cs.backend.Get(r.Context(), storageKey)
	if err != nil {
		log.Printf("error loading cache entry %s: %v", storageKey, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !exists {
		metrics.CacheMiss(cacheID)
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	metrics.CacheHit(cacheID)
	metrics.CacheServedBytes(cacheID, size)

	// Use ServeContent for seekable streams (local files) to support range requests.
	// For non-seekable streams (S3), stream directly without buffering in memory.
	if seeker, ok := file.(io.ReadSeeker); ok {
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, filepath.Base(storageKey), modTime, seeker)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprint(size))
		if !modTime.IsZero() {
			w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
		}
		if _, err := io.Copy(w, file); err != nil {
			log.Printf("error serving %s: %v", storageKey, err)
		}
	}
}

func (cs *CacheServer) handlePut(w http.ResponseWriter, r *http.Request, storageKey string, cacheID string, maxSize int64) {
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
		log.Printf("error storing cache entry %s: %v", storageKey, err)
		http.Error(w, "failed to store cache entry", http.StatusInternalServerError)
		return
	}

	metrics.CacheEntryStored(cacheID)
	metrics.CacheStoredBytes(cacheID, r.ContentLength)

	w.Header().Set("Content-Length", fmt.Sprint(r.ContentLength))
	w.WriteHeader(http.StatusCreated)
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

func (cs *CacheServer) instrument(handlerName string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sw := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}
		metrics.InFlightInc()
		defer metrics.InFlightDec()
		start := time.Now()

		next(sw, r)

		cacheID := ""
		if id, ok := cacheIDFromPath(r.URL.Path); ok {
			cacheID = id
		}

		statusLabel := fmt.Sprint(sw.status)
		duration := time.Since(start)
		metrics.ObserveRequest(r.Method, handlerName, statusLabel, cacheID, duration)

		log.Printf("[%s] %s %s -> %d (%s)", handlerName, r.Method, r.URL.Path, sw.status, duration)
	}
}

func cacheIDFromPath(urlPath string) (string, bool) {
	cacheID, _, err := parseCachePath(urlPath)
	return cacheID, err == nil
}
