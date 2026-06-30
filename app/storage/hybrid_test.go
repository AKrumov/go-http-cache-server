package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

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

func stringReadCloser(s string) io.ReadCloser {
	return io.NopCloser(bytes.NewReader([]byte(s)))
}

// spyBackend records which methods were called and with which keys.
type spyBackend struct {
	mockBackend
	headKeys   []string
	getKeys    []string
	putKeys    []string
	deleteKeys []string
}

func (s *spyBackend) Head(ctx context.Context, key string) (int64, bool, error) {
	s.headKeys = append(s.headKeys, key)
	return s.mockBackend.Head(ctx, key)
}

func (s *spyBackend) Get(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
	s.getKeys = append(s.getKeys, key)
	return s.mockBackend.Get(ctx, key)
}

func (s *spyBackend) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	s.putKeys = append(s.putKeys, key)
	return s.mockBackend.Put(ctx, key, r, size)
}

func (s *spyBackend) Delete(ctx context.Context, key string) error {
	s.deleteKeys = append(s.deleteKeys, key)
	return s.mockBackend.Delete(ctx, key)
}

func TestHybridHead(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		local      *mockBackend
		s3         *mockBackend
		wantSize   int64
		wantExists bool
		wantErr    error
	}{
		{
			name: "local exists",
			local: &mockBackend{
				headFunc: func(ctx context.Context, key string) (int64, bool, error) { return 42, true, nil },
			},
			s3: &mockBackend{
				headFunc: func(ctx context.Context, key string) (int64, bool, error) {
					t.Fatal("s3 should not be consulted when local exists")
					return 0, false, nil
				},
			},
			wantSize:   42,
			wantExists: true,
		},
		{
			name: "local missing s3 exists",
			local: &mockBackend{
				headFunc: func(ctx context.Context, key string) (int64, bool, error) { return 0, false, nil },
			},
			s3: &mockBackend{
				headFunc: func(ctx context.Context, key string) (int64, bool, error) { return 99, true, nil },
			},
			wantSize:   99,
			wantExists: true,
		},
		{
			name: "both missing",
			local: &mockBackend{
				headFunc: func(ctx context.Context, key string) (int64, bool, error) { return 0, false, nil },
			},
			s3: &mockBackend{
				headFunc: func(ctx context.Context, key string) (int64, bool, error) { return 0, false, nil },
			},
			wantExists: false,
		},
		{
			name: "local error propagated",
			local: &mockBackend{
				headFunc: func(ctx context.Context, key string) (int64, bool, error) {
					return 0, false, errors.New("local head error")
				},
			},
			wantErr: errors.New("local head error"),
		},
		{
			name: "s3 error propagated",
			local: &mockBackend{
				headFunc: func(ctx context.Context, key string) (int64, bool, error) { return 0, false, nil },
			},
			s3: &mockBackend{
				headFunc: func(ctx context.Context, key string) (int64, bool, error) {
					return 0, false, errors.New("s3 head error")
				},
			},
			wantErr: errors.New("s3 head error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := NewHybrid(tt.local, tt.s3, HybridOptions{})
			size, exists, err := h.Head(ctx, "k")
			if tt.wantErr != nil {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("got error %v, want containing %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if exists != tt.wantExists {
				t.Fatalf("exists = %v, want %v", exists, tt.wantExists)
			}
			if exists && size != tt.wantSize {
				t.Fatalf("size = %d, want %d", size, tt.wantSize)
			}
		})
	}
}

func TestHybridGetLocalExists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := &spyBackend{
		mockBackend: mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return stringReadCloser("local"), 5, time.Unix(1, 0), true, nil
			},
		},
	}
	s3 := &spyBackend{
		mockBackend: mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				t.Fatal("s3 should not be consulted when local exists")
				return nil, 0, time.Time{}, false, nil
			},
		},
	}
	h := NewHybrid(local, s3, HybridOptions{})

	rc, size, modTime, exists, err := h.Get(ctx, "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists || size != 5 || modTime != time.Unix(1, 0) {
		t.Fatalf("unexpected metadata: exists=%v size=%d modTime=%v", exists, size, modTime)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != "local" {
		t.Fatalf("got %q, want \"local\"", string(data))
	}
	if len(s3.getKeys) != 0 {
		t.Fatalf("s3 Get was called %d times, want 0", len(s3.getKeys))
	}
}

func TestHybridGetBackfillsFromS3(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var putData []byte
	local := &spyBackend{
		mockBackend: mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				if len(putData) == 0 {
					return nil, 0, time.Time{}, false, nil
				}
				return stringReadCloser(string(putData)), int64(len(putData)), time.Unix(2, 0), true, nil
			},
			putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
				data, _ := io.ReadAll(r)
				putData = data
				return nil
			},
		},
	}
	s3 := &spyBackend{
		mockBackend: mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return stringReadCloser("from-s3"), 7, time.Unix(3, 0), true, nil
			},
		},
	}
	h := NewHybrid(local, s3, HybridOptions{})

	rc, size, modTime, exists, err := h.Get(ctx, "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists || size != 7 {
		t.Fatalf("unexpected metadata: exists=%v size=%d", exists, size)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != "from-s3" {
		t.Fatalf("got %q, want \"from-s3\"", string(data))
	}
	if string(putData) != "from-s3" {
		t.Fatalf("local backfill got %q, want \"from-s3\"", string(putData))
	}
	if modTime != time.Unix(2, 0) {
		t.Fatalf("modTime = %v, want time.Unix(2, 0)", modTime)
	}
	if len(local.putKeys) != 1 || local.putKeys[0] != "k" {
		t.Fatalf("local Put keys = %v, want [k]", local.putKeys)
	}
}

func TestHybridGetMissingEverywhere(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := &spyBackend{
		mockBackend: mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return nil, 0, time.Time{}, false, nil
			},
		},
	}
	s3 := &spyBackend{
		mockBackend: mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return nil, 0, time.Time{}, false, nil
			},
		},
	}
	h := NewHybrid(local, s3, HybridOptions{})

	_, _, _, exists, err := h.Get(ctx, "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatal("expected not exists")
	}
}

func TestHybridGetS3Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	wantErr := errors.New("s3 get error")
	local := &spyBackend{
		mockBackend: mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return nil, 0, time.Time{}, false, nil
			},
			putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
				t.Fatal("local put should not be called when S3 errors")
				return nil
			},
		},
	}
	s3 := &spyBackend{
		mockBackend: mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return nil, 0, time.Time{}, false, wantErr
			},
		},
	}
	h := NewHybrid(local, s3, HybridOptions{})

	_, _, _, _, err := h.Get(ctx, "k")
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v, want %v", err, wantErr)
	}
}

func TestHybridGetS3ReadError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := &spyBackend{
		mockBackend: mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return nil, 0, time.Time{}, false, nil
			},
			putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
				_, err := io.ReadAll(r)
				return err
			},
		},
	}
	s3 := &spyBackend{
		mockBackend: mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return io.NopCloser(&failingReader{}), 5, time.Time{}, true, nil
			},
		},
	}
	h := NewHybrid(local, s3, HybridOptions{})

	_, _, _, _, err := h.Get(ctx, "k")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHybridGetBackfillError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	wantErr := errors.New("local put error")
	local := &spyBackend{
		mockBackend: mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return nil, 0, time.Time{}, false, nil
			},
			putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
				return wantErr
			},
		},
	}
	s3 := &spyBackend{
		mockBackend: mockBackend{
			getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
				return stringReadCloser("data"), 4, time.Time{}, true, nil
			},
		},
	}
	h := NewHybrid(local, s3, HybridOptions{})

	_, _, _, _, err := h.Get(ctx, "k")
	if err == nil || !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("got error %v, want containing %v", err, wantErr)
	}
}

func TestHybridPut(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		local        *mockBackend
		s3           *mockBackend
		wantErr      error
		wantLocalPut bool
		wantS3Put    bool
	}{
		{
			name: "writes to both backends",
			local: &mockBackend{
				putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
					data, _ := io.ReadAll(r)
					if string(data) != "payload" || size != 7 {
						t.Errorf("local put got %q size=%d", string(data), size)
					}
					return nil
				},
				getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
					return stringReadCloser("payload"), 7, time.Time{}, true, nil
				},
			},
			s3: &mockBackend{
				putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
					data, _ := io.ReadAll(r)
					if string(data) != "payload" || size != 7 {
						t.Errorf("s3 put got %q size=%d", string(data), size)
					}
					return nil
				},
			},
			wantLocalPut: true,
			wantS3Put:    true,
		},
		{
			name: "local fails s3 not called",
			local: &mockBackend{
				putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
					return errors.New("local put error")
				},
			},
			s3: &mockBackend{
				putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
					t.Fatal("s3 should not be called when local fails")
					return nil
				},
			},
			wantErr:      errors.New("local put error"),
			wantLocalPut: true,
		},
		{
			name: "s3 fails local remains",
			local: &mockBackend{
				putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error { return nil },
				getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
					return stringReadCloser("payload"), 7, time.Time{}, true, nil
				},
			},
			s3: &mockBackend{
				putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error {
					return errors.New("s3 put error")
				},
			},
			wantErr:      errors.New("s3 put error"),
			wantLocalPut: true,
			wantS3Put:    true,
		},
		{
			name: "local get fails after put",
			local: &mockBackend{
				putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error { return nil },
				getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
					return nil, 0, time.Time{}, false, errors.New("local get error")
				},
			},
			s3:           &mockBackend{},
			wantErr:      errors.New("local get error"),
			wantLocalPut: true,
		},
		{
			name: "local entry disappears after put",
			local: &mockBackend{
				putFunc: func(ctx context.Context, key string, r io.Reader, size int64) error { return nil },
				getFunc: func(ctx context.Context, key string) (io.ReadCloser, int64, time.Time, bool, error) {
					return nil, 0, time.Time{}, false, nil
				},
			},
			s3:           &mockBackend{},
			wantErr:      errors.New("disappeared"),
			wantLocalPut: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			localSpy := &spyBackend{mockBackend: *tt.local}
			s3Spy := &spyBackend{mockBackend: *tt.s3}
			h := NewHybrid(localSpy, s3Spy, HybridOptions{})

			err := h.Put(ctx, "k", bytes.NewReader([]byte("payload")), 7)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("got no error, want containing %v", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("got error %v, want containing %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := len(localSpy.putKeys) > 0; got != tt.wantLocalPut {
				t.Fatalf("local put called = %v, want %v", got, tt.wantLocalPut)
			}
			if got := len(s3Spy.putKeys) > 0; got != tt.wantS3Put {
				t.Fatalf("s3 put called = %v, want %v", got, tt.wantS3Put)
			}
		})
	}
}

func TestHybridDelete(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		local           *mockBackend
		s3              *mockBackend
		wantErr         error
		wantLocalDelete bool
		wantS3Delete    bool
	}{
		{
			name:            "deletes from both backends",
			local:           &mockBackend{},
			s3:              &mockBackend{},
			wantLocalDelete: true,
			wantS3Delete:    true,
		},
		{
			name: "local error propagated",
			local: &mockBackend{
				deleteFunc: func(ctx context.Context, key string) error {
					return errors.New("local delete error")
				},
			},
			s3:              &mockBackend{},
			wantErr:         errors.New("local delete error"),
			wantLocalDelete: true,
			wantS3Delete:    true,
		},
		{
			name:  "s3 error propagated",
			local: &mockBackend{},
			s3: &mockBackend{
				deleteFunc: func(ctx context.Context, key string) error {
					return errors.New("s3 delete error")
				},
			},
			wantErr:         errors.New("s3 delete error"),
			wantLocalDelete: true,
			wantS3Delete:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			localSpy := &spyBackend{mockBackend: *tt.local}
			s3Spy := &spyBackend{mockBackend: *tt.s3}
			h := NewHybrid(localSpy, s3Spy, HybridOptions{})

			err := h.Delete(ctx, "k")
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("got no error, want containing %v", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("got error %v, want containing %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := len(localSpy.deleteKeys) > 0; got != tt.wantLocalDelete {
				t.Fatalf("local delete called = %v, want %v", got, tt.wantLocalDelete)
			}
			if got := len(s3Spy.deleteKeys) > 0; got != tt.wantS3Delete {
				t.Fatalf("s3 delete called = %v, want %v", got, tt.wantS3Delete)
			}
		})
	}
}

type failingReader struct{}

func (e *failingReader) Read(p []byte) (int, error) {
	return 0, errors.New("read error")
}
