package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"go_gradle_cache/app/metrics"
	"go_gradle_cache/app/storage"
)

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gradle-cache-server", flag.ContinueOnError)

	var (
		showVersion bool
		listenAddr  string
		storageType string
		cacheDir    string
		s3Bucket    string
		s3Prefix    string
		s3Region    string
		s3Endpoint  string
		maxUpload   int64
	)

	fs.BoolVar(&showVersion, "version", false, "print version and exit")
	fs.StringVar(&listenAddr, "listen", ":8080", "address to listen on")
	fs.StringVar(&storageType, "storage", envOrDefault("STORAGE_TYPE", "local"), "storage backend: local or s3")
	fs.StringVar(&cacheDir, "dir", envOrDefault("LOCAL_DIR", "./cache-data"), "directory to store cache entries (local storage)")
	fs.StringVar(&s3Bucket, "s3-bucket", envOrDefault("S3_BUCKET", ""), "S3 bucket name")
	fs.StringVar(&s3Prefix, "s3-prefix", envOrDefault("S3_PREFIX", ""), "S3 key prefix")
	fs.StringVar(&s3Region, "s3-region", envOrDefault("S3_REGION", ""), "S3 region")
	fs.StringVar(&s3Endpoint, "s3-endpoint", envOrDefault("S3_ENDPOINT", ""), "S3 endpoint URL (for MinIO, etc.)")
	fs.Int64Var(&maxUpload, "max-upload", 0, "max upload size per cache entry in bytes (0 = unlimited)")
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

	var backend storage.Backend
	var err error

	switch strings.ToLower(storageType) {
	case "local":
		backend, err = storage.NewLocal(cacheDir)
		if err != nil {
			return fmt.Errorf("failed to create local storage: %w", err)
		}
	case "s3":
		backend, err = storage.NewS3(ctx, storage.S3Options{
			Bucket:   s3Bucket,
			Prefix:   s3Prefix,
			Region:   s3Region,
			Endpoint: s3Endpoint,
		})
		if err != nil {
			return fmt.Errorf("failed to create S3 storage: %w", err)
		}
	default:
		return fmt.Errorf("unknown storage type: %s", storageType)
	}

	server := NewCacheServer(backend, maxUpload)
	mux := http.NewServeMux()
	mux.HandleFunc("/cache/", server.instrument("cache", server.handleCache))
	mux.HandleFunc("/health", server.instrument("health", server.handleHealth))
	mux.Handle("/metrics", metrics.Handler())

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
