package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"go_http_cache_server/metrics"
	"go_http_cache_server/storage"
)

var errIncompleteAuthConfig = errors.New("both auth username and auth password are required when HTTP authentication is enabled")

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// parseDurationWithDays parses a duration string supporting Go durations
// plus an optional "d"/"D" suffix for days (e.g. "7d").
func parseDurationWithDays(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	if strings.HasSuffix(s, "d") || strings.HasSuffix(s, "D") {
		days, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid day duration %q: %w", s, err)
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	return time.ParseDuration(s)
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("go-http-cache-server", flag.ContinueOnError)

	var (
		showVersion bool
		listenAddr  string
		storageType string
		cacheDir    string
		s3Bucket      string
		s3Prefix      string
		s3Region      string
		s3Endpoint    string
		s3Concurrency int
		maxUpload     int64
		authUser    string
		authPass    string
	)

	localTTL, err := parseDurationWithDays(envOrDefault("LOCAL_TTL", "0"))
	if err != nil {
		return fmt.Errorf("invalid LOCAL_TTL: %w", err)
	}
	localCleanupInterval, err := parseDurationWithDays(envOrDefault("LOCAL_CLEANUP_INTERVAL", "24h"))
	if err != nil {
		return fmt.Errorf("invalid LOCAL_CLEANUP_INTERVAL: %w", err)
	}

	fs.BoolVar(&showVersion, "version", false, "print version and exit")
	fs.StringVar(&listenAddr, "listen", ":8080", "address to listen on")
	fs.StringVar(&storageType, "storage", envOrDefault("STORAGE_TYPE", "local"), "storage backend: local, s3, or hybrid")
	fs.StringVar(&cacheDir, "dir", envOrDefault("LOCAL_DIR", "./cache-data"), "directory to store cache entries (local storage)")
	fs.StringVar(&s3Bucket, "s3-bucket", envOrDefault("S3_BUCKET", ""), "S3 bucket name")
	fs.StringVar(&s3Prefix, "s3-prefix", envOrDefault("S3_PREFIX", ""), "S3 key prefix")
	fs.StringVar(&s3Region, "s3-region", envOrDefault("S3_REGION", ""), "S3 region")
	fs.StringVar(&s3Endpoint, "s3-endpoint", envOrDefault("S3_ENDPOINT", ""), "S3 endpoint URL (for MinIO, etc.)")
	fs.IntVar(&s3Concurrency, "s3-concurrency", 0, "S3 upload concurrency (0 = default)")
	fs.Int64Var(&maxUpload, "max-upload", 0, "max upload size per cache entry in bytes (0 = unlimited)")
	fs.StringVar(&authUser, "auth-username", envOrDefault("AUTH_USERNAME", ""), "HTTP Basic authentication username (disabled when empty)")
	fs.StringVar(&authPass, "auth-password", envOrDefault("AUTH_PASSWORD", ""), "HTTP Basic authentication password (disabled when empty)")
	fs.DurationVar(&localTTL, "local-ttl", localTTL, "local cache TTL based on last access time (e.g. 24h, 7d, 0=disabled)")
	fs.DurationVar(&localCleanupInterval, "local-cleanup-interval", localCleanupInterval, "interval between local cache cleanup runs (e.g. 1h, 1d)")
	if v := os.Getenv("S3_CONCURRENCY"); v != "" && s3Concurrency == 0 {
		if n, err := strconv.Atoi(v); err == nil {
			s3Concurrency = n
		}
	}
	if v := os.Getenv("MAX_UPLOAD_SIZE"); v != "" && maxUpload == 0 {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			maxUpload = n
		}
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if showVersion {
		fmt.Println(version)
		return nil
	}
	auth, err := newAuthConfig(authUser, authPass)
	if err != nil {
		return err
	}

	var backend storage.Backend

	switch strings.ToLower(storageType) {
	case "local":
		localBackend, err := storage.NewLocal(cacheDir)
		if err != nil {
			return fmt.Errorf("failed to create local storage: %w", err)
		}
		backend = localBackend
		if localTTL > 0 {
			localBackend.StartCleanup(ctx, localTTL, localCleanupInterval)
		}
	case "s3":
		backend, err = storage.NewS3(ctx, storage.S3Options{
			Bucket:      s3Bucket,
			Prefix:      s3Prefix,
			Region:      s3Region,
			Endpoint:    s3Endpoint,
			Concurrency: s3Concurrency,
		})
		if err != nil {
			return fmt.Errorf("failed to create S3 storage: %w", err)
		}
	case "hybrid":
		if s3Bucket == "" {
			return fmt.Errorf("s3-bucket is required when storage type is hybrid")
		}
		localBackend, err := storage.NewLocal(cacheDir)
		if err != nil {
			return fmt.Errorf("failed to create local storage: %w", err)
		}
		s3Backend, err := storage.NewS3(ctx, storage.S3Options{
			Bucket:      s3Bucket,
			Prefix:      s3Prefix,
			Region:      s3Region,
			Endpoint:    s3Endpoint,
			Concurrency: s3Concurrency,
		})
		if err != nil {
			return fmt.Errorf("failed to create S3 storage: %w", err)
		}
		backend = storage.NewHybrid(localBackend, s3Backend)
		if localTTL > 0 {
			localBackend.StartCleanup(ctx, localTTL, localCleanupInterval)
		}
	default:
		return fmt.Errorf("unknown storage type: %s", storageType)
	}

	server := NewCacheServerWithAuth(backend, maxUpload, auth)
	mux := http.NewServeMux()
	mux.HandleFunc("/cache/", server.instrument("cache", server.requireAuth(server.handleCache)))
	mux.HandleFunc("/health", server.instrument("health", server.handleHealth))
	mux.Handle("/metrics", server.requireAuth(metrics.Handler().ServeHTTP))

	srv := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("Gradle remote build cache server %s listening on %s, storage=%s", version, listenAddr, storageType)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
