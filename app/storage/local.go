package storage

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go_http_cache_server/metrics"
)

// fileHandle abstracts *os.File for testability.
type fileHandle interface {
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
	Close() error
	Stat() (os.FileInfo, error)
}

// localFS abstracts filesystem operations for testability.
type localFS interface {
	mkdirAll(path string, perm os.FileMode) error
	create(name string) (fileHandle, error)
	open(name string) (fileHandle, error)
	stat(name string) (os.FileInfo, error)
	rename(oldpath, newpath string) error
	remove(name string) error
}

type realFS struct{}

func (realFS) mkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (realFS) create(name string) (fileHandle, error) {
	f, err := os.Create(name)
	if err != nil {
		return nil, err
	}
	return realFile{f}, nil
}
func (realFS) open(name string) (fileHandle, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	return realFile{f}, nil
}
func (realFS) stat(name string) (os.FileInfo, error) { return os.Stat(name) }
func (realFS) rename(oldpath, newpath string) error  { return os.Rename(oldpath, newpath) }
func (realFS) remove(name string) error              { return os.Remove(name) }

type realFile struct{ *os.File }

func (r realFile) Read(p []byte) (int, error)  { return r.File.Read(p) }
func (r realFile) Write(p []byte) (int, error) { return r.File.Write(p) }
func (r realFile) Close() error                { return r.File.Close() }
func (r realFile) Stat() (os.FileInfo, error)  { return r.File.Stat() }

// ------------------------------------------------------------------
// Sharded locks for improved concurrency
// ------------------------------------------------------------------

const shardCount = 256

func shardIndex(key string) uint32 {
	// Simple FNV-1a hash for sharding
	h := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return h % shardCount
}

type shardedLocks struct {
	locks [shardCount]sync.RWMutex
}

func (sl *shardedLocks) rLock(key string) {
	sl.locks[shardIndex(key)].RLock()
}

func (sl *shardedLocks) rUnlock(key string) {
	sl.locks[shardIndex(key)].RUnlock()
}

func (sl *shardedLocks) lock(key string) {
	sl.locks[shardIndex(key)].Lock()
}

func (sl *shardedLocks) unlock(key string) {
	sl.locks[shardIndex(key)].Unlock()
}

// Local implements Backend using the local filesystem.
type Local struct {
	root string
	mu   shardedLocks
	fs   localFS
}

// NewLocal creates a new local filesystem backend.
func NewLocal(root string) (*Local, error) {
	return newLocal(root, realFS{})
}

func newLocal(root string, fs localFS) (*Local, error) {
	if err := fs.mkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create local cache directory: %w", err)
	}
	return &Local{root: root, fs: fs}, nil
}

func (l *Local) path(key string) string {
	return filepath.Join(l.root, filepath.FromSlash(key))
}

// Head implements Backend.
func (l *Local) Head(ctx context.Context, key string) (size int64, exists bool, err error) {
	l.mu.rLock(key)
	defer l.mu.rUnlock(key)

	info, err := l.fs.stat(l.path(key))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return info.Size(), true, nil
}

// Get implements Backend.
func (l *Local) Get(ctx context.Context, key string) (rc io.ReadCloser, size int64, modTime time.Time, exists bool, err error) {
	l.mu.rLock(key)
	defer l.mu.rUnlock(key)

	f, err := l.fs.open(l.path(key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, time.Time{}, false, nil
		}
		return nil, 0, time.Time{}, false, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, time.Time{}, false, err
	}

	return f, info.Size(), info.ModTime(), true, nil
}

// Delete implements Backend.
func (l *Local) Delete(ctx context.Context, key string) error {
	l.mu.lock(key)
	defer l.mu.unlock(key)

	err := l.fs.remove(l.path(key))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete cache entry: %w", err)
	}
	return nil
}

// Put implements Backend.
func (l *Local) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	filePath := l.path(key)
	tmpPath := filePath + ".tmp"

	if err := l.fs.mkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fmt.Errorf("unable to create cache directory: %w", err)
	}

	tmpFile, err := l.fs.create(tmpPath)
	if err != nil {
		return fmt.Errorf("unable to write cache file: %w", err)
	}

	// Use pooled buffer for reduced GC pressure
	buf := getBuffer()
	_, copyErr := io.CopyBuffer(tmpFile, r, *buf)
	putBuffer(buf)

	if copyErr != nil {
		tmpFile.Close()
		l.fs.remove(tmpPath)
		return fmt.Errorf("failed to store cache entry: %w", copyErr)
	}

	if err := tmpFile.Close(); err != nil {
		l.fs.remove(tmpPath)
		return fmt.Errorf("failed to finalize cache entry: %w", err)
	}

	l.mu.lock(key)
	defer l.mu.unlock(key)

	if err := l.fs.rename(tmpPath, filePath); err != nil {
		l.fs.remove(tmpPath)
		return fmt.Errorf("unable to persist cache entry: %w", err)
	}

	return nil
}

// StartCleanup launches a background goroutine that deletes local cache
// entries whose last access time is older than ttl. It runs immediately,
// then every interval, and stops when ctx is canceled.
func (l *Local) StartCleanup(ctx context.Context, ttl, interval time.Duration) {
	go func() {
		l.runCleanup(ttl)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				l.runCleanup(ttl)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (l *Local) runCleanup(ttl time.Duration) {
	metrics.LocalCleanupRun()
	now := time.Now()
	var totalEvictedBytes int64
	var totalEntries int64
	var totalSize int64

	err := filepath.WalkDir(l.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			slog.Warn("error reading file info for cleanup", "path", path, "error", err)
			return nil
		}
		size := info.Size()
		totalEntries++
		totalSize += size

		at, err := accessTime(path)
		if err != nil {
			slog.Warn("error reading access time for cleanup", "path", path, "error", err)
			return nil
		}

		if now.Sub(at) > ttl {
			if err := l.fs.remove(path); err != nil {
				slog.Error("error evicting stale cache entry", "path", path, "error", err)
			} else {
				slog.Info("evicted stale cache entry", "path", path, "last_access", at.Format(time.RFC3339), "size", size)
				totalEvictedBytes += size
			}
		}
		return nil
	})
	if err != nil {
		slog.Error("error walking local cache for cleanup", "error", err)
	}

	metrics.LocalCleanupEvicted(float64(totalEvictedBytes))
	metrics.SetLocalCacheEntries(float64(totalEntries))
	metrics.SetLocalCacheSize(float64(totalSize))
}
