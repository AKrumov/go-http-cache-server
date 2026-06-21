package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mockFileInfo implements os.FileInfo for tests.
type mockFileInfo struct {
	size    int64
	modTime time.Time
}

func (m mockFileInfo) Name() string       { return "test" }
func (m mockFileInfo) Size() int64        { return m.size }
func (m mockFileInfo) Mode() os.FileMode  { return 0o644 }
func (m mockFileInfo) ModTime() time.Time { return m.modTime }
func (m mockFileInfo) IsDir() bool        { return false }
func (m mockFileInfo) Sys() interface{}   { return nil }

// mockFile implements fileHandle for tests.
type mockFile struct {
	data     []byte
	pos      int
	statErr  error
	closeErr error
	closed   bool
}

func (m *mockFile) Read(p []byte) (int, error) {
	if m.pos >= len(m.data) {
		return 0, io.EOF
	}
	n := copy(p, m.data[m.pos:])
	m.pos += n
	return n, nil
}

func (m *mockFile) Write(p []byte) (int, error) {
	m.data = append(m.data, p...)
	return len(p), nil
}

func (m *mockFile) Close() error {
	m.closed = true
	return m.closeErr
}

func (m *mockFile) Stat() (os.FileInfo, error) {
	if m.statErr != nil {
		return nil, m.statErr
	}
	return mockFileInfo{size: int64(len(m.data)), modTime: time.Now()}, nil
}

// mockLocalFS implements localFS for tests.
type mockLocalFS struct {
	mkdirAllFunc func(path string, perm os.FileMode) error
	createFunc   func(name string) (fileHandle, error)
	openFunc     func(name string) (fileHandle, error)
	statFunc     func(name string) (os.FileInfo, error)
	renameFunc   func(oldpath, newpath string) error
	removeFunc   func(name string) error
}

func (m *mockLocalFS) mkdirAll(path string, perm os.FileMode) error {
	if m.mkdirAllFunc != nil {
		return m.mkdirAllFunc(path, perm)
	}
	return nil
}
func (m *mockLocalFS) create(name string) (fileHandle, error) {
	if m.createFunc != nil {
		return m.createFunc(name)
	}
	return &mockFile{}, nil
}
func (m *mockLocalFS) open(name string) (fileHandle, error) {
	if m.openFunc != nil {
		return m.openFunc(name)
	}
	return &mockFile{}, nil
}
func (m *mockLocalFS) stat(name string) (os.FileInfo, error) {
	if m.statFunc != nil {
		return m.statFunc(name)
	}
	return nil, os.ErrNotExist
}
func (m *mockLocalFS) rename(oldpath, newpath string) error {
	if m.renameFunc != nil {
		return m.renameFunc(oldpath, newpath)
	}
	return nil
}
func (m *mockLocalFS) remove(name string) error {
	if m.removeFunc != nil {
		return m.removeFunc(name)
	}
	return nil
}

func TestNewLocal(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Parallel()
		fs := &mockLocalFS{
			mkdirAllFunc: func(path string, perm os.FileMode) error { return nil },
		}
		l, err := newLocal("/tmp/cache", fs)
		if err != nil {
			t.Fatal(err)
		}
		if l.root != "/tmp/cache" {
			t.Fatalf("root = %q", l.root)
		}
	})

	t.Run("mkdirAll fails", func(t *testing.T) {
		t.Parallel()
		fs := &mockLocalFS{
			mkdirAllFunc: func(path string, perm os.FileMode) error {
				return errors.New("no permission")
			},
		}
		_, err := newLocal("/tmp/cache", fs)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestLocalHead(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		t.Parallel()
		fs := &mockLocalFS{
			statFunc: func(name string) (os.FileInfo, error) {
				return mockFileInfo{size: 42}, nil
			},
		}
		l, _ := newLocal("/tmp", fs)
		size, exists, err := l.Head(context.Background(), "myid/key")
		if err != nil {
			t.Fatal(err)
		}
		if !exists || size != 42 {
			t.Fatalf("exists=%v size=%d", exists, size)
		}
	})

	t.Run("not exists", func(t *testing.T) {
		t.Parallel()
		fs := &mockLocalFS{
			statFunc: func(name string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
		}
		l, _ := newLocal("/tmp", fs)
		_, exists, err := l.Head(context.Background(), "myid/key")
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatal("expected not exists")
		}
	})

	t.Run("stat error", func(t *testing.T) {
		t.Parallel()
		fs := &mockLocalFS{
			statFunc: func(name string) (os.FileInfo, error) {
				return nil, errors.New("io error")
			},
		}
		l, _ := newLocal("/tmp", fs)
		_, _, err := l.Head(context.Background(), "myid/key")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestLocalGet(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		t.Parallel()
		fs := &mockLocalFS{
			openFunc: func(name string) (fileHandle, error) {
				return &mockFile{data: []byte("hello")}, nil
			},
		}
		l, _ := newLocal("/tmp", fs)
		rc, size, _, exists, err := l.Get(context.Background(), "myid/key")
		if err != nil {
			t.Fatal(err)
		}
		if !exists || size != 5 {
			t.Fatalf("exists=%v size=%d", exists, size)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		if string(data) != "hello" {
			t.Fatalf("data = %q", string(data))
		}
	})

	t.Run("not exists", func(t *testing.T) {
		fs := &mockLocalFS{
			openFunc: func(name string) (fileHandle, error) {
				return nil, os.ErrNotExist
			},
		}
		l, _ := newLocal("/tmp", fs)
		_, _, _, exists, err := l.Get(context.Background(), "myid/key")
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatal("expected not exists")
		}
	})

	t.Run("open error", func(t *testing.T) {
		t.Parallel()
		fs := &mockLocalFS{
			openFunc: func(name string) (fileHandle, error) {
				return nil, errors.New("io error")
			},
		}
		l, _ := newLocal("/tmp", fs)
		_, _, _, _, err := l.Get(context.Background(), "myid/key")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("stat error", func(t *testing.T) {
		fs := &mockLocalFS{
			openFunc: func(name string) (fileHandle, error) {
				return &mockFile{statErr: errors.New("stat fail")}, nil
			},
		}
		l, _ := newLocal("/tmp", fs)
		_, _, _, _, err := l.Get(context.Background(), "myid/key")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestLocalPut(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Parallel()
		fs := &mockLocalFS{}
		l, _ := newLocal("/tmp", fs)
		err := l.Put(context.Background(), "myid/key", strings.NewReader("data"), 4)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("mkdirAll fails", func(t *testing.T) {
		fs := &mockLocalFS{
			mkdirAllFunc: func(path string, perm os.FileMode) error {
				if path == "/tmp" {
					return nil
				}
				return errors.New("fail")
			},
		}
		l, _ := newLocal("/tmp", fs)
		err := l.Put(context.Background(), "myid/key", strings.NewReader("data"), 4)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("create fails", func(t *testing.T) {
		t.Parallel()
		fs := &mockLocalFS{
			createFunc: func(name string) (fileHandle, error) {
				return nil, errors.New("fail")
			},
		}
		l, _ := newLocal("/tmp", fs)
		err := l.Put(context.Background(), "myid/key", strings.NewReader("data"), 4)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("copy fails", func(t *testing.T) {
		t.Parallel()
		fs := &mockLocalFS{}
		l, _ := newLocal("/tmp", fs)
		err := l.Put(context.Background(), "myid/key", &errorReader{err: errors.New("fail")}, 4)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("close fails", func(t *testing.T) {
		t.Parallel()
		fs := &mockLocalFS{
			createFunc: func(name string) (fileHandle, error) {
				return &mockFile{closeErr: errors.New("close fail")}, nil
			},
		}
		l, _ := newLocal("/tmp", fs)
		err := l.Put(context.Background(), "myid/key", strings.NewReader("data"), 4)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rename fails", func(t *testing.T) {
		t.Parallel()
		fs := &mockLocalFS{
			renameFunc: func(oldpath, newpath string) error {
				return errors.New("rename fail")
			},
		}
		l, _ := newLocal("/tmp", fs)
		err := l.Put(context.Background(), "myid/key", strings.NewReader("data"), 4)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestLocalRealFS(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	key := "myid/key"

	// Put
	if err := l.Put(ctx, key, strings.NewReader("hello"), 5); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Head
	size, exists, err := l.Head(ctx, key)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if !exists || size != 5 {
		t.Fatalf("Head = %d, %v", size, exists)
	}

	// Get
	rc, size, _, exists, err := l.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !exists || size != 5 {
		t.Fatalf("Get = %d, %v", size, exists)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != "hello" {
		t.Fatalf("data = %q", string(data))
	}

	// Not found
	_, exists, _ = l.Head(ctx, "missing")
	if exists {
		t.Fatal("expected not found")
	}
}

// errorReader is an io.Reader that always fails.
type errorReader struct {
	err error
}

func (e *errorReader) Read([]byte) (int, error) {
	return 0, e.err
}


func TestLocalRunCleanup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()

	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}

	freshKey := "cache/fresh.bin"
	staleKey := "cache/stale.bin"
	nestedStaleKey := "cache/nested/deep.bin"

	// Store entries.
	for _, key := range []string{freshKey, staleKey, nestedStaleKey} {
		if err := l.Put(ctx, key, bytes.NewReader([]byte("data")), 4); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}

	// Make some access times old.
	oldAtime := time.Now().Add(-48 * time.Hour)
	for _, key := range []string{staleKey, nestedStaleKey} {
		if err := os.Chtimes(l.path(key), oldAtime, time.Now()); err != nil {
			t.Fatalf("Chtimes %s: %v", key, err)
		}
	}

	// Run cleanup with a 24-hour TTL.
	l.runCleanup(24 * time.Hour)

	// Stale files should be gone, fresh file should remain.
	for _, key := range []string{staleKey, nestedStaleKey} {
		if _, err := os.Stat(l.path(key)); !os.IsNotExist(err) {
			t.Fatalf("stale file %s should have been removed, got err=%v", key, err)
		}
	}
	if _, err := os.Stat(l.path(freshKey)); err != nil {
		t.Fatalf("fresh file should remain: %v", err)
	}
}

func TestLocalRunCleanupEmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Should not panic or error on an empty directory.
	l.runCleanup(time.Hour)
}

func TestLocalStartCleanup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}

	key := "cache/old.bin"
	if err := l.Put(ctx, key, bytes.NewReader([]byte("old")), 3); err != nil {
		t.Fatalf("Put: %v", err)
	}
	path := l.path(key)
	if err := os.Chtimes(path, time.Now().Add(-2*time.Hour), time.Now()); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	l.StartCleanup(ctx, 1*time.Hour, 50*time.Millisecond)

	// Wait for the initial cleanup run to complete.
	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale file should have been removed by background cleanup, got err=%v", err)
	}

	cancel()
}

func TestLocalStartCleanupStopsOnContextDone(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())

	dir := t.TempDir()
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}

	l.StartCleanup(ctx, time.Hour, time.Millisecond)

	// Give the goroutine a chance to start, then cancel it.
	time.Sleep(10 * time.Millisecond)
	cancel()

	// If the goroutine does not stop, this test will leak a goroutine.
	// The test itself passes if we reach here without panic.
}


func TestAccessTime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	at, err := accessTime(path)
	if err != nil {
		t.Fatalf("accessTime: %v", err)
	}
	if at.IsZero() {
		t.Fatal("access time is zero")
	}
	if time.Since(at) > time.Minute {
		t.Fatalf("access time too old: %v", at)
	}

	// Setting an explicit access time should be reflected.
	oldAtime := time.Now().Add(-7 * 24 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, oldAtime, time.Now()); err != nil {
		t.Fatal(err)
	}

	at, err = accessTime(path)
	if err != nil {
		t.Fatalf("accessTime after Chtimes: %v", err)
	}
	// Allow a small delta for filesystem precision.
	if at.Before(oldAtime.Add(-time.Second)) || at.After(oldAtime.Add(time.Second)) {
		t.Fatalf("access time = %v, want near %v", at, oldAtime)
	}
}
