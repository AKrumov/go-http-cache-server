package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// mockBackend is a test implementation of storage.Backend.
type mockBackend struct {
	headFunc func(ctx context.Context, key string) (int64, bool, error)
	getFunc  func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error)
	putFunc  func(ctx context.Context, key string, r io.Reader, size int64) error
}

func (m *mockBackend) Head(ctx context.Context, key string) (int64, bool, error) {
	if m.headFunc != nil {
		return m.headFunc(ctx, key)
	}
	return 0, false, nil
}

func (m *mockBackend) Get(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, key)
	}
	return nil, 0, time.Time{}, false, nil
}

func (m *mockBackend) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if m.putFunc != nil {
		return m.putFunc(ctx, key, r, size)
	}
	return nil
}

func TestParseCachePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantID  string
		wantKey string
		wantErr bool
	}{
		{"valid simple", "/cache/myid/abc", "myid", "abc", false},
		{"valid nested", "/cache/myid/foo/bar", "myid", "foo/bar", false},
		{"valid with dots in key", "/cache/myid/foo.txt", "myid", "foo.txt", false},
		{"empty string", "", "", "", true},
		{"no prefix", "/foo/bar", "", "", true},
		{"empty after prefix", "/cache/", "", "", true},
		{"contains dotdot", "/cache/myid/../secret", "", "", true},
		{"contains backslash", "/cache/myid\\secret", "", "", true},
		{"single part", "/cache/myid", "", "", true},
		{"empty cacheID", "/cache//key", "", "", true},
		{"empty entryKey", "/cache/myid/", "", "", true},
		{"extra slashes", "/cache/myid//foo", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, key, err := parseCachePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseCachePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
			if !tt.wantErr {
				if id != tt.wantID || key != tt.wantKey {
					t.Fatalf("parseCachePath(%q) = (%q, %q), want (%q, %q)", tt.path, id, key, tt.wantID, tt.wantKey)
				}
			}
		})
	}
}

func TestMakeStorageKey(t *testing.T) {
	if got := makeStorageKey("myid", "foo/bar"); got != "myid/foo/bar" {
		t.Fatalf("makeStorageKey = %q, want myid/foo/bar", got)
	}
}

func TestStatusResponseWriter(t *testing.T) {
	t.Run("write header once", func(t *testing.T) {
		rr := httptest.NewRecorder()
		sw := &statusResponseWriter{ResponseWriter: rr, status: http.StatusOK}
		sw.WriteHeader(http.StatusNotFound)
		if sw.status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", sw.status, http.StatusNotFound)
		}
		if !sw.wroteHeader {
			t.Fatal("wroteHeader should be true")
		}
		if rr.Code != http.StatusNotFound {
			t.Fatalf("recorder code = %d, want %d", rr.Code, http.StatusNotFound)
		}
	})

	t.Run("write header twice is idempotent", func(t *testing.T) {
		rr := httptest.NewRecorder()
		sw := &statusResponseWriter{ResponseWriter: rr, status: http.StatusOK}
		sw.WriteHeader(http.StatusNotFound)
		sw.WriteHeader(http.StatusInternalServerError)
		if sw.status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", sw.status, http.StatusNotFound)
		}
		if rr.Code != http.StatusNotFound {
			t.Fatalf("recorder code = %d, want %d", rr.Code, http.StatusNotFound)
		}
	})

	t.Run("write triggers default 200", func(t *testing.T) {
		rr := httptest.NewRecorder()
		sw := &statusResponseWriter{ResponseWriter: rr, status: http.StatusOK}
		_, _ = sw.Write([]byte("hi"))
		if !sw.wroteHeader {
			t.Fatal("wroteHeader should be true")
		}
		if sw.status != http.StatusOK {
			t.Fatalf("status = %d, want %d", sw.status, http.StatusOK)
		}
	})

	t.Run("write after explicit header", func(t *testing.T) {
		rr := httptest.NewRecorder()
		sw := &statusResponseWriter{ResponseWriter: rr, status: http.StatusOK}
		sw.WriteHeader(http.StatusCreated)
		_, _ = sw.Write([]byte("hi"))
		if sw.status != http.StatusCreated {
			t.Fatalf("status = %d, want %d", sw.status, http.StatusCreated)
		}
	})
}

type errorReader struct {
	err error
}

func (e *errorReader) Read([]byte) (int, error) {
	return 0, e.err
}

func TestCacheServerHandleHead(t *testing.T) {
	t.Run("hit", func(t *testing.T) {
		be := &mockBackend{
			headFunc: func(ctx context.Context, key string) (int64, bool, error) {
				return 42, true, nil
			},
		}
		cs := NewCacheServer(be, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/cache/myid/foo", nil)
		cs.handleCache(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusOK)
		}
		if rr.Header().Get("Content-Length") != "42" {
			t.Fatalf("Content-Length = %q, want 42", rr.Header().Get("Content-Length"))
		}
	})

	t.Run("miss", func(t *testing.T) {
		be := &mockBackend{
			headFunc: func(ctx context.Context, key string) (int64, bool, error) {
				return 0, false, nil
			},
		}
		cs := NewCacheServer(be, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/cache/myid/foo", nil)
		cs.handleCache(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusNotFound)
		}
	})

	t.Run("error", func(t *testing.T) {
		be := &mockBackend{
			headFunc: func(ctx context.Context, key string) (int64, bool, error) {
				return 0, false, errors.New("fail")
			},
		}
		cs := NewCacheServer(be, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/cache/myid/foo", nil)
		cs.handleCache(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusInternalServerError)
		}
	})

	t.Run("invalid path", func(t *testing.T) {
		cs := NewCacheServer(&mockBackend{}, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/cache/", nil)
		cs.handleCache(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})
}

func TestCacheServerHandleGet(t *testing.T) {
	t.Run("hit seekable", func(t *testing.T) {
		be := &mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return io.NopCloser(strings.NewReader("data")), 4, time.Now(), true, nil
			},
		}
		cs := NewCacheServer(be, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/cache/myid/foo", nil)
		cs.handleCache(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusOK)
		}
		if rr.Body.String() != "data" {
			t.Fatalf("body = %q, want data", rr.Body.String())
		}
	})

	t.Run("hit non-seekable", func(t *testing.T) {
		be := &mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return &readCloser{Reader: strings.NewReader("data")}, 4, time.Now(), true, nil
			},
		}
		cs := NewCacheServer(be, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/cache/myid/foo", nil)
		cs.handleCache(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusOK)
		}
		if rr.Body.String() != "data" {
			t.Fatalf("body = %q, want data", rr.Body.String())
		}
	})

	t.Run("miss", func(t *testing.T) {
		be := &mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return nil, 0, time.Time{}, false, nil
			},
		}
		cs := NewCacheServer(be, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/cache/myid/foo", nil)
		cs.handleCache(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusNotFound)
		}
	})

	t.Run("error", func(t *testing.T) {
		be := &mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return nil, 0, time.Time{}, false, errors.New("fail")
			},
		}
		cs := NewCacheServer(be, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/cache/myid/foo", nil)
		cs.handleCache(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusInternalServerError)
		}
	})

	t.Run("invalid path", func(t *testing.T) {
		cs := NewCacheServer(&mockBackend{}, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/cache/", nil)
		cs.handleCache(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})
}

// readCloser wraps an io.Reader without providing io.Seeker.
type readCloser struct {
	io.Reader
}

func (r *readCloser) Close() error { return nil }

func TestCacheServerHandlePut(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		be := &mockBackend{
			putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
				data, _ := io.ReadAll(r)
				if string(data) != "payload" {
					t.Fatalf("payload = %q, want payload", string(data))
				}
				return nil
			},
		}
		cs := NewCacheServer(be, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/cache/myid/foo", strings.NewReader("payload"))
		req.ContentLength = 7
		cs.handleCache(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusCreated)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		cs := NewCacheServer(&mockBackend{}, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/cache/myid/foo", strings.NewReader(""))
		req.ContentLength = 0
		cs.handleCache(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})

	t.Run("body too large", func(t *testing.T) {
		cs := NewCacheServer(&mockBackend{}, 100)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/cache/myid/foo", strings.NewReader("x"))
		req.ContentLength = 101
		cs.handleCache(rr, req)
		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
		}
	})

	t.Run("backend error", func(t *testing.T) {
		be := &mockBackend{
			putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
				return errors.New("fail")
			},
		}
		cs := NewCacheServer(be, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/cache/myid/foo", strings.NewReader("payload"))
		req.ContentLength = 7
		cs.handleCache(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusInternalServerError)
		}
	})

	t.Run("invalid path", func(t *testing.T) {
		cs := NewCacheServer(&mockBackend{}, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/cache/", nil)
		cs.handleCache(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusBadRequest)
		}
	})
}

func TestCacheServerInvalidMethod(t *testing.T) {
	cs := NewCacheServer(&mockBackend{}, 0)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/cache/myid/foo", nil)
	cs.handleCache(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestCacheServerBasicAuth(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		be := &mockBackend{
			headFunc: func(ctx context.Context, key string) (int64, bool, error) {
				return 1, true, nil
			},
		}
		cs := NewCacheServer(be, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/cache/myid/foo", nil)

		cs.requireAuth(cs.handleCache)(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusOK)
		}
	})

	t.Run("requires credentials", func(t *testing.T) {
		auth, err := newAuthConfig("gradle", "secret")
		if err != nil {
			t.Fatal(err)
		}
		cs := NewCacheServerWithAuth(&mockBackend{}, 0, auth)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/cache/myid/foo", nil)

		cs.requireAuth(cs.handleCache)(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusUnauthorized)
		}
		if rr.Header().Get("WWW-Authenticate") == "" {
			t.Fatal("expected WWW-Authenticate header")
		}
	})

	t.Run("accepts valid credentials", func(t *testing.T) {
		auth, err := newAuthConfig("gradle", "secret")
		if err != nil {
			t.Fatal(err)
		}
		be := &mockBackend{
			headFunc: func(ctx context.Context, key string) (int64, bool, error) {
				return 1, true, nil
			},
		}
		cs := NewCacheServerWithAuth(be, 0, auth)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/cache/myid/foo", nil)
		req.SetBasicAuth("gradle", "secret")

		cs.requireAuth(cs.handleCache)(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusOK)
		}
	})
}

func TestHandleHealth(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		cs := NewCacheServer(&mockBackend{}, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		cs.handleHealth(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusOK)
		}
		if rr.Body.String() != "ok" {
			t.Fatalf("body = %q, want ok", rr.Body.String())
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		cs := NewCacheServer(&mockBackend{}, 0)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/health", nil)
		cs.handleHealth(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("code = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
		}
	})
}

func TestInstrument(t *testing.T) {
	cs := NewCacheServer(&mockBackend{}, 0)
	called := false
	handler := cs.instrument("test", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cache/myid/foo", nil)
	handler(rr, req)
	if !called {
		t.Fatal("handler was not called")
	}
	if rr.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want %d", rr.Code, http.StatusAccepted)
	}
}

func TestEnvOrDefault(t *testing.T) {
	key := "TEST_ENV_OR_DEFAULT_XYZ"
	os.Unsetenv(key)
	if got := envOrDefault(key, "default"); got != "default" {
		t.Fatalf("envOrDefault = %q, want default", got)
	}
	os.Setenv(key, "set")
	defer os.Unsetenv(key)
	if got := envOrDefault(key, "default"); got != "set" {
		t.Fatalf("envOrDefault = %q, want set", got)
	}
}

func TestVersionFlag(t *testing.T) {
	old := version
	version = "v1.2.3"
	defer func() { version = old }()

	err := run(context.Background(), []string{"-version"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLocal(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = run(ctx, []string{"-listen", ":28080", "-storage", "local", "-dir", dir})
	}()

	time.Sleep(200 * time.Millisecond)
	resp, err := http.Get("http://localhost:28080/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	cancel()
}

func TestRunS3(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = run(ctx, []string{
			"-listen", ":28081",
			"-storage", "s3",
			"-s3-bucket", "test-bucket",
			"-s3-endpoint", srv.URL,
		})
	}()

	time.Sleep(200 * time.Millisecond)
	resp, err := http.Get("http://localhost:28081/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	cancel()
}

func TestRunUnknownStorage(t *testing.T) {
	err := run(context.Background(), []string{"-storage", "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown storage")
	}
}

func TestRunListenError(t *testing.T) {
	err := run(context.Background(), []string{"-listen", ":abc", "-storage", "local", "-dir", t.TempDir()})
	if err == nil {
		t.Fatal("expected error for invalid listen address")
	}
}

func TestRunFlagParseError(t *testing.T) {
	err := run(context.Background(), []string{"-unknown-flag"})
	if err == nil {
		t.Fatal("expected error for invalid flag")
	}
}

func TestRunLocalStorageError(t *testing.T) {
	f, err := os.CreateTemp("", "notadir")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	err = run(context.Background(), []string{"-storage", "local", "-dir", f.Name()})
	if err == nil {
		t.Fatal("expected error for invalid cache dir")
	}
}

func TestRunS3MissingBucket(t *testing.T) {
	err := run(context.Background(), []string{"-storage", "s3"})
	if err == nil {
		t.Fatal("expected error for missing S3 bucket")
	}
}

func TestRunIncompleteAuthConfig(t *testing.T) {
	err := run(context.Background(), []string{"-auth-username", "gradle"})
	if err == nil {
		t.Fatal("expected error for incomplete auth config")
	}
}
