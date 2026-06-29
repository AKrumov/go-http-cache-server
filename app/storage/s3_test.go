package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials"
)

func mockS3Server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Path-style URLs: /bucket/key...
		path := r.URL.Path

		switch r.Method {
		case http.MethodHead:
			if strings.Contains(path, "/not-found/") || path == "/test-bucket/missing" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if strings.Contains(path, "/error/") {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Length", "42")
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusOK)

		case http.MethodGet:
			if strings.Contains(path, "/not-found/") || path == "/test-bucket/missing" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if strings.Contains(path, "/error/") {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Length", "5")
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("hello"))

		case http.MethodPut:
			if strings.Contains(path, "/error/") {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)

		case http.MethodDelete:
			if strings.Contains(path, "/error/") {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	return httptest.NewServer(mux)
}

func TestNewS3(t *testing.T) {
	t.Run("missing bucket", func(t *testing.T) {
		_, err := NewS3(context.Background(), S3Options{})
		if err == nil {
			t.Fatal("expected error for missing bucket")
		}
	})

	t.Run("success", func(t *testing.T) {
		srv := mockS3Server()
		defer srv.Close()

		_, err := NewS3(context.Background(), S3Options{
			Bucket:   "test-bucket",
			Region:   "us-east-1",
			Endpoint: srv.URL,
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestS3Head(t *testing.T) {
	srv := mockS3Server()
	defer srv.Close()

	be, err := NewS3(context.Background(), S3Options{
		Bucket:      "test-bucket",
		Region:      "us-east-1",
		Endpoint:    srv.URL,
		Credentials: credentials.NewStaticCredentialsProvider("TEST", "TEST", ""),
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		key        string
		wantExists bool
		wantSize   int64
		wantErr    bool
	}{
		{"exists", "found", true, 42, false},
		{"not found", "missing", false, 0, false},
		{"error", "error/500", false, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size, exists, err := be.Head(context.Background(), tt.key)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Head(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
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

func TestS3Get(t *testing.T) {
	srv := mockS3Server()
	defer srv.Close()

	be, err := NewS3(context.Background(), S3Options{
		Bucket:      "test-bucket",
		Region:      "us-east-1",
		Endpoint:    srv.URL,
		Credentials: credentials.NewStaticCredentialsProvider("TEST", "TEST", ""),
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		key        string
		wantExists bool
		wantBody   string
		wantErr    bool
	}{
		{"exists", "found", true, "hello", false},
		{"not found", "missing", false, "", false},
		{"error", "error/500", false, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc, _, _, exists, err := be.Get(context.Background(), tt.key)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Get(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
			if exists != tt.wantExists {
				t.Fatalf("exists = %v, want %v", exists, tt.wantExists)
			}
			if tt.wantBody != "" {
				data, _ := io.ReadAll(rc)
				rc.Close()
				if string(data) != tt.wantBody {
					t.Fatalf("body = %q, want %q", string(data), tt.wantBody)
				}
			}
		})
	}
}

func TestS3Put(t *testing.T) {
	srv := mockS3Server()
	defer srv.Close()

	be, err := NewS3(context.Background(), S3Options{
		Bucket:      "test-bucket",
		Region:      "us-east-1",
		Endpoint:    srv.URL,
		Credentials: credentials.NewStaticCredentialsProvider("TEST", "TEST", ""),
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"success", "found", false},
		{"error", "error/500", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := be.Put(context.Background(), tt.key, strings.NewReader("data"), 4)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Put(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
		})
	}
}

func TestS3Key(t *testing.T) {
	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	be, err := NewS3(context.Background(), S3Options{
		Bucket:      "test-bucket",
		Prefix:      "prefix",
		Region:      "us-east-1",
		Endpoint:    ts.URL,
		Credentials: credentials.NewStaticCredentialsProvider("TEST", "TEST", ""),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = be.Head(context.Background(), "mykey")
	if err != nil {
		t.Fatalf("Head error: %v", err)
	}

	wantPath := "/test-bucket/prefix/mykey"
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
}

func TestS3PrefixEmpty(t *testing.T) {
	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	be, err := NewS3(context.Background(), S3Options{
		Bucket:      "test-bucket",
		Region:      "us-east-1",
		Endpoint:    ts.URL,
		Credentials: credentials.NewStaticCredentialsProvider("TEST", "TEST", ""),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = be.Head(context.Background(), "mykey")
	if err != nil {
		t.Fatalf("Head error: %v", err)
	}

	wantPath := "/test-bucket/mykey"
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
}

func TestIsNotFound(t *testing.T) {
	// Test with a real S3 404 response via mock server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `<Error><Code>NoSuchKey</Code></Error>`)
	}))
	defer srv.Close()

	be, err := NewS3(context.Background(), S3Options{
		Bucket:      "test-bucket",
		Region:      "us-east-1",
		Endpoint:    srv.URL,
		Credentials: credentials.NewStaticCredentialsProvider("TEST", "TEST", ""),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, exists, err := be.Head(context.Background(), "missing")
	if err != nil {
		t.Fatalf("expected no error for 404, got %v", err)
	}
	if exists {
		t.Fatal("expected not exists")
	}
}

func TestIsNotFoundPlainError(t *testing.T) {
	if isNotFound(errors.New("plain error")) {
		t.Fatal("expected false for plain error")
	}
}

func TestS3Delete(t *testing.T) {
	srv := mockS3Server()
	defer srv.Close()

	be, err := NewS3(context.Background(), S3Options{
		Bucket:      "test-bucket",
		Region:      "us-east-1",
		Endpoint:    srv.URL,
		Credentials: credentials.NewStaticCredentialsProvider("TEST", "TEST", ""),
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"success", "found", false},
		{"error", "error/500", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := be.Delete(context.Background(), tt.key)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Delete(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
		})
	}
}

func TestS3HeadNoContentLength(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	be, err := NewS3(context.Background(), S3Options{
		Bucket:      "test-bucket",
		Region:      "us-east-1",
		Endpoint:    ts.URL,
		Credentials: credentials.NewStaticCredentialsProvider("TEST", "TEST", ""),
	})
	if err != nil {
		t.Fatal(err)
	}

	size, exists, err := be.Head(context.Background(), "mykey")
	if err != nil {
		t.Fatalf("Head error: %v", err)
	}
	if !exists || size != 0 {
		t.Fatalf("exists=%v size=%d", exists, size)
	}
}
