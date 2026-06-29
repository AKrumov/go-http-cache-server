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
	headFunc   func(ctx context.Context, key string) (int64, bool, error)
	getFunc    func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error)
	putFunc    func(ctx context.Context, key string, r io.Reader, size int64) error
	deleteFunc func(ctx context.Context, key string) error
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

func (m *mockBackend) Delete(ctx context.Context, key string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, key)
	}
	return nil
}

// cacheRequest executes a cache request against a CacheServer and returns the recorder.
func cacheRequest(cs *CacheServer, method, path string, body io.Reader) *httptest.ResponseRecorder {
	return cacheRequestWithLen(cs, method, path, body, -1)
}

// cacheRequestWithLen executes a cache request with an explicit Content-Length.
func cacheRequestWithLen(cs *CacheServer, method, path string, body io.Reader, contentLen int64) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, body)
	if contentLen >= 0 {
		req.ContentLength = contentLen
	}
	cs.handleCache(rr, req)
	return rr
}

func assertStatus(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rr.Code != want {
		t.Errorf("status = %d, want %d", rr.Code, want)
	}
}

func assertHeader(t *testing.T, rr *httptest.ResponseRecorder, key, want string) {
	t.Helper()
	if got := rr.Header().Get(key); got != want {
		t.Errorf("%s = %q, want %q", key, got, want)
	}
}

func assertBody(t *testing.T, rr *httptest.ResponseRecorder, want string) {
	t.Helper()
	if got := rr.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
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
			t.Parallel()
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

func TestSafeCacheID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"myid", "myid"},
		{"my-id_1.0", "my-id_1.0"},
		{"", "invalid"},
		{"../../../etc", "invalid"},
		{"my id", "invalid"},
		{"my/id", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			if got := safeCacheID(tt.input); got != tt.want {
				t.Fatalf("safeCacheID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMakeStorageKey(t *testing.T) {
	t.Parallel()
	if got := makeStorageKey("myid", "foo/bar"); got != "myid/foo/bar" {
		t.Fatalf("makeStorageKey = %q, want myid/foo/bar", got)
	}
}

func TestStatusResponseWriter(t *testing.T) {
	t.Run("write header once", func(t *testing.T) {
		t.Parallel()
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
		t.Parallel()
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
		t.Parallel()
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
		t.Parallel()
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
	tests := []struct {
		name       string
		backend    *mockBackend
		path       string
		wantStatus int
		wantLen    string
	}{
		{
			name: "hit",
			backend: &mockBackend{
				headFunc: func(ctx context.Context, key string) (int64, bool, error) {
					return 42, true, nil
				},
			},
			path:       "/cache/myid/foo",
			wantStatus: http.StatusOK,
			wantLen:    "42",
		},
		{
			name: "miss",
			backend: &mockBackend{
				headFunc: func(ctx context.Context, key string) (int64, bool, error) {
					return 0, false, nil
				},
			},
			path:       "/cache/myid/foo",
			wantStatus: http.StatusNotFound,
		},
		{
			name: "error",
			backend: &mockBackend{
				headFunc: func(ctx context.Context, key string) (int64, bool, error) {
					return 0, false, errors.New("fail")
				},
			},
			path:       "/cache/myid/foo",
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "invalid path",
			backend:    &mockBackend{},
			path:       "/cache/",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cs := NewCacheServer(tt.backend, 0)
			rr := cacheRequest(cs, http.MethodHead, tt.path, nil)
			assertStatus(t, rr, tt.wantStatus)
			if tt.wantLen != "" {
				assertHeader(t, rr, "Content-Length", tt.wantLen)
			}
		})
	}
}

func TestCacheServerHandleGet(t *testing.T) {
	tests := []struct {
		name       string
		backend    *mockBackend
		path       string
		wantStatus int
		wantBody   string
		wantLen    string
	}{
		{
			name: "hit seekable",
			backend: &mockBackend{
				getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
					return io.NopCloser(strings.NewReader("data")), 4, time.Now(), true, nil
				},
			},
			path:       "/cache/myid/foo",
			wantStatus: http.StatusOK,
			wantBody:   "data",
		},
		{
			name: "hit non-seekable",
			backend: &mockBackend{
				getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
					return &readCloser{Reader: strings.NewReader("data")}, 4, time.Now(), true, nil
				},
			},
			path:       "/cache/myid/foo",
			wantStatus: http.StatusOK,
			wantBody:   "data",
			wantLen:    "4",
		},
		{
			name: "miss",
			backend: &mockBackend{
				getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
					return nil, 0, time.Time{}, false, nil
				},
			},
			path:       "/cache/myid/foo",
			wantStatus: http.StatusNotFound,
		},
		{
			name: "error",
			backend: &mockBackend{
				getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
					return nil, 0, time.Time{}, false, errors.New("fail")
				},
			},
			path:       "/cache/myid/foo",
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "invalid path",
			backend:    &mockBackend{},
			path:       "/cache/",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cs := NewCacheServer(tt.backend, 0)
			rr := cacheRequest(cs, http.MethodGet, tt.path, nil)
			assertStatus(t, rr, tt.wantStatus)
			if tt.wantBody != "" {
				assertBody(t, rr, tt.wantBody)
			}
			if tt.wantLen != "" {
				assertHeader(t, rr, "Content-Length", tt.wantLen)
			}
		})
	}
}

// readCloser wraps an io.Reader without providing io.Seeker.
type readCloser struct {
	io.Reader
}

func (r *readCloser) Close() error { return nil }

func TestCacheServerHandlePut(t *testing.T) {
	tests := []struct {
		name       string
		backend    *mockBackend
		maxUpload  int64
		path       string
		body       string
		contentLen int64
		wantStatus int
		wantBody   string
	}{
		// default path is set in the loop
		{
			name: "success",
			backend: &mockBackend{
				putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
					data, _ := io.ReadAll(r)
					if string(data) != "payload" {
						t.Errorf("payload = %q, want payload", string(data))
					}
					return nil
				},
			},
			body:       "payload",
			contentLen: 7,
			wantStatus: http.StatusCreated,
			wantBody:   "payload",
		},
		{
			name:       "empty body",
			backend:    &mockBackend{},
			body:       "",
			contentLen: 0,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "body too large",
			backend:    &mockBackend{},
			maxUpload:  100,
			body:       "x",
			contentLen: 101,
			wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name: "backend error",
			backend: &mockBackend{
				putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
					return errors.New("fail")
				},
			},
			body:       "payload",
			contentLen: 7,
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "invalid path",
			backend:    &mockBackend{},
			path:       "/cache/",
			body:       "payload",
			contentLen: 7,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cs := NewCacheServer(tt.backend, tt.maxUpload)
			path := tt.path
			if path == "" {
				path = "/cache/myid/foo"
			}
			rr := cacheRequestWithLen(cs, http.MethodPut, path, strings.NewReader(tt.body), tt.contentLen)
			assertStatus(t, rr, tt.wantStatus)
		})
	}
}

func TestCacheServerHandleDelete(t *testing.T) {
	tests := []struct {
		name       string
		backend    *mockBackend
		path       string
		wantStatus int
	}{
		{
			name: "success",
			backend: &mockBackend{
				deleteFunc: func(ctx context.Context, key string) error {
					if key != "myid/foo" {
						t.Errorf("delete key = %q, want myid/foo", key)
					}
					return nil
				},
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "backend error",
			backend: &mockBackend{
				deleteFunc: func(ctx context.Context, key string) error {
					return errors.New("fail")
				},
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "invalid path",
			backend:    &mockBackend{},
			path:       "/cache/",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cs := NewCacheServer(tt.backend, 0)
			path := tt.path
			if path == "" {
				path = "/cache/myid/foo"
			}
			rr := cacheRequest(cs, http.MethodDelete, path, nil)
			assertStatus(t, rr, tt.wantStatus)
		})
	}
}

func TestCacheServerInvalidMethod(t *testing.T) {
	t.Parallel()
	cs := NewCacheServer(&mockBackend{}, 0)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/cache/myid/foo", nil)
	cs.handleCache(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestCacheServerHandlePutChunkedRejected(t *testing.T) {
	t.Parallel()
	cs := NewCacheServer(&mockBackend{}, 0)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/cache/myid/foo", strings.NewReader("payload"))
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}
	cs.handleCache(rr, req)
	if rr.Code != http.StatusLengthRequired {
		t.Fatalf("code = %d, want %d", rr.Code, http.StatusLengthRequired)
	}
}

func TestCacheServerBasicAuth(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		t.Parallel()
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
		t.Parallel()
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
		t.Parallel()
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
		t.Parallel()
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
		t.Parallel()
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
	t.Parallel()
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

func TestRunHybrid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = run(ctx, []string{
			"-listen", ":28082",
			"-storage", "hybrid",
			"-dir", t.TempDir(),
			"-s3-bucket", "test-bucket",
			"-s3-endpoint", srv.URL,
		})
	}()

	time.Sleep(200 * time.Millisecond)
	resp, err := http.Get("http://localhost:28082/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	cancel()
}

func TestRunHybridMissingBucket(t *testing.T) {
	err := run(context.Background(), []string{"-storage", "hybrid", "-dir", t.TempDir()})
	if err == nil {
		t.Fatal("expected error for missing S3 bucket in hybrid mode")
	}
}

func TestParseDurationWithDays(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"empty", "", 0, false},
		{"zero", "0", 0, false},
		{"hours", "24h", 24 * time.Hour, false},
		{"days", "7d", 7 * 24 * time.Hour, false},
		{"days uppercase", "3D", 3 * 24 * time.Hour, false},
		{"fractional days", "1.5d", 36 * time.Hour, false},
		{"minutes", "30m", 30 * time.Minute, false},
		{"invalid", "not-a-duration", 0, true},
		{"invalid days", "xd", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseDurationWithDays(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseDurationWithDays(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("parseDurationWithDays(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestRunIncompleteAuthConfig(t *testing.T) {
	err := run(context.Background(), []string{"-auth-username", "gradle"})
	if err == nil {
		t.Fatal("expected error for incomplete auth config")
	}
}
