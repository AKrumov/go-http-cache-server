package storage

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// AsyncUploadItem represents a pending S3 upload.
type AsyncUploadItem struct {
	Key      string
	Size     int64
	Attempts int
}

// AsyncUploader manages a background queue for uploading cache entries to S3.
// This is used by the Hybrid backend to return 201 immediately after local
// storage and upload to S3 in the background.
type AsyncUploader struct {
	backend Backend // the S3 backend
	local   Backend // the local backend (to read from)
	queue   chan AsyncUploadItem
	wg      sync.WaitGroup
	mu      sync.RWMutex
	running bool
	maxRetry int
}

// NewAsyncUploader creates an async uploader with a bounded queue.
// queueSize is the maximum number of pending uploads. If the queue is full,
// new uploads are dropped (best-effort).
func NewAsyncUploader(s3Backend, localBackend Backend, queueSize int, maxRetry int) *AsyncUploader {
	if queueSize <= 0 {
		queueSize = 1000
	}
	if maxRetry <= 0 {
		maxRetry = 3
	}
	return &AsyncUploader{
		backend:  s3Backend,
		local:    localBackend,
		queue:    make(chan AsyncUploadItem, queueSize),
		maxRetry: maxRetry,
	}
}

// Start begins the background worker(s) that consume the upload queue.
// Call this once after construction.
func (a *AsyncUploader) Start(workers int) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return
	}
	a.running = true
	a.mu.Unlock()

	if workers <= 0 {
		workers = 2
	}
	for i := 0; i < workers; i++ {
		a.wg.Add(1)
		go a.worker()
	}
}

// Stop signals the uploader to stop and waits for in-flight uploads to finish.
func (a *AsyncUploader) Stop() {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return
	}
	a.running = false
	a.mu.Unlock()

	close(a.queue)
	a.wg.Wait()
}

// Enqueue adds an item to the upload queue. Returns true if enqueued, false if dropped.
func (a *AsyncUploader) Enqueue(key string, size int64) bool {
	a.mu.RLock()
	running := a.running
	a.mu.RUnlock()
	if !running {
		return false
	}
	select {
	case a.queue <- AsyncUploadItem{Key: key, Size: size}:
		return true
	default:
		slog.Warn("async upload queue full, dropping item", "key", key)
		return false
	}
}

func (a *AsyncUploader) worker() {
	defer a.wg.Done()
	for item := range a.queue {
		a.upload(item)
	}
}

func (a *AsyncUploader) upload(item AsyncUploadItem) {
	ctx := context.Background()
	localRC, localSize, _, exists, err := a.local.Get(ctx, item.Key)
	if err != nil || !exists {
		slog.Error("async upload failed to read local entry", "key", item.Key, "error", err, "exists", exists)
		return
	}
	defer localRC.Close()

	if err := a.backend.Put(ctx, item.Key, localRC, localSize); err != nil {
		item.Attempts++
		if item.Attempts < a.maxRetry {
			// Re-queue with exponential backoff (simplified: just retry later).
			go func(it AsyncUploadItem) {
				time.Sleep(time.Duration(it.Attempts) * 2 * time.Second)
				// Try to re-enqueue; may drop if queue is full.
				select {
				case a.queue <- it:
				default:
					slog.Error("async upload retry dropped, queue full", "key", it.Key, "attempts", it.Attempts)
				}
			}(item)
		} else {
			slog.Error("async upload failed after max retries", "key", item.Key, "error", err, "attempts", item.Attempts)
		}
		return
	}
	slog.Debug("async upload succeeded", "key", item.Key, "size", localSize)
}
