package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
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

// Local implements Backend using the local filesystem.
type Local struct {
	root string
	mu   sync.RWMutex
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
	l.mu.RLock()
	defer l.mu.RUnlock()

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
	l.mu.RLock()
	defer l.mu.RUnlock()

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

	if _, err := io.Copy(tmpFile, r); err != nil {
		tmpFile.Close()
		l.fs.remove(tmpPath)
		return fmt.Errorf("failed to store cache entry: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		l.fs.remove(tmpPath)
		return fmt.Errorf("failed to finalize cache entry: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.fs.rename(tmpPath, filePath); err != nil {
		l.fs.remove(tmpPath)
		return fmt.Errorf("unable to persist cache entry: %w", err)
	}

	return nil
}
