package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// Hybrid implements Backend by combining a local filesystem cache with S3.
//
// Reads are local-first: if an entry exists locally it is served directly.
// If it is missing locally but exists in S3, the object is downloaded to
// local storage and then served from local.
//
// Writes persist the entry to both local storage and S3. When async upload
// is enabled, the S3 upload happens in the background after the local write
// returns, reducing PUT latency.
type Hybrid struct {
	local       Backend
	s3          Backend
	asyncUpload *AsyncUploader
}

// HybridOptions holds configuration for the hybrid backend.
type HybridOptions struct {
	AsyncUpload     bool // if true, S3 uploads are done in background
	AsyncQueueSize  int  // max pending async uploads
	AsyncMaxRetry   int  // max retries for async uploads
	AsyncWorkers    int  // number of background upload workers
}

// NewHybrid creates a new hybrid backend.
func NewHybrid(local, s3 Backend, opts HybridOptions) *Hybrid {
	h := &Hybrid{local: local, s3: s3}
	if opts.AsyncUpload {
		h.asyncUpload = NewAsyncUploader(s3, local, opts.AsyncQueueSize, opts.AsyncMaxRetry)
		h.asyncUpload.Start(opts.AsyncWorkers)
	}
	return h
}

// StopAsyncUploader gracefully shuts down the background upload worker.
// Call this during server shutdown.
func (h *Hybrid) StopAsyncUploader() {
	if h.asyncUpload != nil {
		h.asyncUpload.Stop()
	}
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

	// Stream the S3 object directly to local storage instead of buffering it
	// in memory. This keeps memory usage flat regardless of object size.
	if err := h.local.Put(ctx, key, s3RC, s3Size); err != nil {
		return nil, 0, time.Time{}, false, fmt.Errorf("failed to backfill cache entry locally: %w", err)
	}

	return h.local.Get(ctx, key)
}

// Delete removes the object from both local storage and S3.
// Errors from either backend are reported, with local errors taking precedence.
func (h *Hybrid) Delete(ctx context.Context, key string) error {
	var localErr, s3Err error
	if err := h.local.Delete(ctx, key); err != nil {
		localErr = fmt.Errorf("failed to delete local cache entry: %w", err)
	}
	if err := h.s3.Delete(ctx, key); err != nil {
		s3Err = fmt.Errorf("failed to delete S3 cache entry: %w", err)
	}
	if localErr != nil {
		return localErr
	}
	return s3Err
}

// Put stores an object in both local storage and S3.
// If async upload is enabled, the S3 upload happens in the background.
func (h *Hybrid) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := h.local.Put(ctx, key, r, size); err != nil {
		return fmt.Errorf("failed to store cache entry locally: %w", err)
	}

	if h.asyncUpload != nil {
		if h.asyncUpload.Enqueue(key, size) {
			slog.Debug("async S3 upload queued", "key", key, "size", size)
		} else {
			slog.Warn("async S3 upload queue full, falling back to sync", "key", key)
			// Fall back to sync upload
			if err := h.syncS3Upload(ctx, key); err != nil {
				return err
			}
		}
		return nil
	}

	return h.syncS3Upload(ctx, key)
}

func (h *Hybrid) syncS3Upload(ctx context.Context, key string) error {
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

