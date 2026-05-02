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

	t.Run("exists", func(t *testing.T) {
		size, exists, err := be.Head(context.Background(), "found")
		if err != nil {
			t.Fatal(err)
		}
		if !exists || size != 42 {
			t.Fatalf("exists=%v size=%d", exists, size)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, exists, err := be.Head(context.Background(), "missing")
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatal("expected not found")
		}
	})

	t.Run("error", func(t *testing.T) {
		_, _, err := be.Head(context.Background(), "error/500")
		if err == nil {
			t.Fatal("expected error")
		}
	})
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

	t.Run("exists", func(t *testing.T) {
		rc, size, _, exists, err := be.Get(context.Background(), "found")
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

	t.Run("not found", func(t *testing.T) {
		_, _, _, exists, err := be.Get(context.Background(), "missing")
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatal("expected not found")
		}
	})

	t.Run("error", func(t *testing.T) {
		_, _, _, _, err := be.Get(context.Background(), "error/500")
		if err == nil {
			t.Fatal("expected error")
		}
	})
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

	t.Run("success", func(t *testing.T) {
		err := be.Put(context.Background(), "found", strings.NewReader("data"), 4)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("error", func(t *testing.T) {
		err := be.Put(context.Background(), "error/500", strings.NewReader("data"), 4)
		if err == nil {
			t.Fatal("expected error")
		}
	})
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
