package storage

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"go_http_cache_server/metrics"
)

// MemoryCacheEntry holds a cached object in memory.
type MemoryCacheEntry struct {
	Data    []byte
	ModTime time.Time
	Size    int64
}

// LRUCache wraps a Backend with an in-memory LRU cache for small objects.
// Only objects at or below maxEntrySize are cached. The total cache size
// is bounded by maxTotalSize.
type LRUCache struct {
	backend      Backend
	lru          *lru.Cache[string, *MemoryCacheEntry]
	maxEntrySize int64
	maxTotalSize int64
}

// NewLRUCache creates an in-memory LRU cache wrapper.
// maxTotalSize is the approximate total memory budget in bytes.
// maxEntrySize is the maximum individual object size to cache.
func NewLRUCache(b Backend, maxTotalSize int64, maxEntrySize int64) (*LRUCache, error) {
	// We estimate entries by assuming average object size of maxEntrySize/2
	estimatedEntries := 1024
	if maxEntrySize > 0 && maxTotalSize > 0 {
		estimatedEntries = int(maxTotalSize / (maxEntrySize / 2))
		if estimatedEntries < 64 {
			estimatedEntries = 64
		}
	}
	cache, err := lru.New[string, *MemoryCacheEntry](estimatedEntries)
	if err != nil {
		return nil, err
	}
	return &LRUCache{
		backend:      b,
		lru:          cache,
		maxEntrySize: maxEntrySize,
		maxTotalSize: maxTotalSize,
	}, nil
}

// Head implements Backend.
func (c *LRUCache) Head(ctx context.Context, key string) (int64, bool, error) {
	if entry, ok := c.lru.Get(key); ok {
		metrics.MemoryCacheHit()
		return entry.Size, true, nil
	}
	return c.backend.Head(ctx, key)
}

// Get implements Backend.
func (c *LRUCache) Get(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
	if entry, ok := c.lru.Get(key); ok {
		metrics.MemoryCacheHit()
		return io.NopCloser(bytes.NewReader(entry.Data)), entry.Size, entry.ModTime, true, nil
	}
	metrics.MemoryCacheMiss()
	rc, size, modTime, exists, err := c.backend.Get(ctx, key)
	if err != nil || !exists {
		return rc, size, modTime, exists, err
	}
	// If small enough, cache it in memory.
	if c.maxEntrySize > 0 && size <= c.maxEntrySize {
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, 0, time.Time{}, false, err
		}
		entry := &MemoryCacheEntry{Data: data, ModTime: modTime, Size: size}
		c.addToCache(key, entry)
		return io.NopCloser(bytes.NewReader(data)), size, modTime, true, nil
	}
	return rc, size, modTime, exists, nil
}

// Put implements Backend.
func (c *LRUCache) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if c.maxEntrySize > 0 && size <= c.maxEntrySize {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		entry := &MemoryCacheEntry{Data: data, ModTime: time.Now(), Size: size}
		c.addToCache(key, entry)
		return c.backend.Put(ctx, key, bytes.NewReader(data), size)
	}
	return c.backend.Put(ctx, key, r, size)
}

// Delete implements Backend.
func (c *LRUCache) Delete(ctx context.Context, key string) error {
	c.lru.Remove(key)
	return c.backend.Delete(ctx, key)
}

func (c *LRUCache) addToCache(key string, entry *MemoryCacheEntry) {
	if c.maxTotalSize > 0 && int64(len(entry.Data)) > c.maxTotalSize {
		return
	}
	c.lru.Add(key, entry)
	slog.Debug("cached entry in memory", "key", key, "size", len(entry.Data))
}

var _ Backend = (*LRUCache)(nil)
