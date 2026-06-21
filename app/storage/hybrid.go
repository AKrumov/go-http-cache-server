package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"
)

// Hybrid implements Backend by combining a local filesystem cache with S3.
//
// Reads are local-first: if an entry exists locally it is served directly.
// If it is missing locally but exists in S3, the object is downloaded to
// local storage and then served from local.
//
// Writes persist the entry to both local storage and S3.
type Hybrid struct {
	local Backend
	s3    Backend
}

// NewHybrid creates a new hybrid backend that uses local as the fast local
// cache and s3 as the remote backing store.
func NewHybrid(local, s3 Backend) *Hybrid {
	return &Hybrid{local: local, s3: s3}
}

// Head checks if an object exists, preferring local metadata.
func (h *Hybrid) Head(ctx context.Context, key string) (size int64, exists bool, err error) {
	size, exists, err = h.local.Head(ctx, key)
	if err != nil || exists {
		return size, exists, err
	}

	return h.s3.Head(ctx, key)
}

// Get retrieves an object, serving from local storage when possible.
// When the object is missing locally but present in S3, it is downloaded
// to local storage first and then served from local.
func (h *Hybrid) Get(ctx context.Context, key string) (rc io.ReadCloser, size int64, modTime time.Time, exists bool, err error) {
	rc, size, modTime, exists, err = h.local.Get(ctx, key)
	if err != nil || exists {
		return rc, size, modTime, exists, err
	}

	s3RC, s3Size, _, s3Exists, err := h.s3.Get(ctx, key)
	if err != nil || !s3Exists {
		return nil, 0, time.Time{}, false, err
	}
	defer s3RC.Close()

	data, err := io.ReadAll(s3RC)
	if err != nil {
		return nil, 0, time.Time{}, false, fmt.Errorf("failed to read object from S3: %w", err)
	}

	if err := h.local.Put(ctx, key, bytes.NewReader(data), s3Size); err != nil {
		return nil, 0, time.Time{}, false, fmt.Errorf("failed to backfill cache entry locally: %w", err)
	}

	return h.local.Get(ctx, key)
}

// Put stores an object in both local storage and S3.
// The local copy is written first, then uploaded to S3 from the local file.
// If the S3 upload fails, the local copy remains in place as a valid cache entry.
func (h *Hybrid) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := h.local.Put(ctx, key, r, size); err != nil {
		return fmt.Errorf("failed to store cache entry locally: %w", err)
	}

	localRC, localSize, _, exists, err := h.local.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to read locally stored cache entry: %w", err)
	}
	if !exists {
		return fmt.Errorf("locally stored cache entry disappeared before S3 upload")
	}
	defer localRC.Close()

	if err := h.s3.Put(ctx, key, localRC, localSize); err != nil {
		return fmt.Errorf("failed to store cache entry in S3: %w", err)
	}

	return nil
}
