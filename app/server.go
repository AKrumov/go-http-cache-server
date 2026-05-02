package main

import "go_gradle_cache/app/storage"

type CacheServer struct {
	backend   storage.Backend
	maxUpload int64
}

// NewCacheServer creates a new cache server with the given backend.
func NewCacheServer(backend storage.Backend, maxUpload int64) *CacheServer {
	return &CacheServer{backend: backend, maxUpload: maxUpload}
}
