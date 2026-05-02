package storage

import (
	"context"
	"errors"
	"io"
	"os"
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
		fs := &mockLocalFS{}
		l, _ := newLocal("/tmp", fs)
		err := l.Put(context.Background(), "myid/key", &errorReader{err: errors.New("fail")}, 4)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("close fails", func(t *testing.T) {
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
