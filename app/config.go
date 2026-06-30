package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"strings"
	"time"

	"go_http_cache_server/health"
	"go_http_cache_server/logging"
	"go_http_cache_server/metrics"
	"go_http_cache_server/middleware"
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
		showVersion          bool
		listenAddr           string
		debugListenAddr      string
		storageType          string
		cacheDir             string
		s3Bucket             string
		s3Prefix             string
		s3Region             string
		s3Endpoint           string
		s3Concurrency        int
		s3RetryMax           int
		maxUpload            int64
		authUser             string
		authPass             string
		logFormat            string
		logLevel             string
		requestTimeout       time.Duration
		shutdownTimeout      time.Duration
		rateLimitPerIP       float64
		rateLimitGlobal      float64
		memCacheSize         int64
		memCacheMaxEntry     int64
		asyncS3Upload        bool
		asyncS3QueueSize     int
		asyncS3Workers       int
		asyncS3MaxRetry      int
		circuitBreakerFailures int
		circuitBreakerTimeout  time.Duration
		tlsCert              string
		tlsKey               string
	)

	localTTL, err := parseDurationWithDays(envOrDefault("LOCAL_TTL", "0"))
	if err != nil {
		return fmt.Errorf("invalid LOCAL_TTL: %w", err)
	}
	localCleanupInterval, err := parseDurationWithDays(envOrDefault("LOCAL_CLEANUP_INTERVAL", "24h"))
	if err != nil {
		return fmt.Errorf("invalid LOCAL_CLEANUP_INTERVAL: %w", err)
	}

	// Logging flags
	fs.StringVar(&logFormat, "log-format", envOrDefault("LOG_FORMAT", "text"), "log format: text or json")
	fs.StringVar(&logLevel, "log-level", envOrDefault("LOG_LEVEL", "info"), "log level: debug, info, warn, error")
	// Server flags
	fs.BoolVar(&showVersion, "version", false, "print version and exit")
	fs.StringVar(&listenAddr, "listen", ":8080", "address to listen on")
	fs.StringVar(&debugListenAddr, "debug-listen", envOrDefault("DEBUG_LISTEN", ""), "address for debug/pprof endpoints (empty = disabled)")
	fs.DurationVar(&requestTimeout, "request-timeout", 30*time.Second, "per-request operation timeout")
	fs.DurationVar(&shutdownTimeout, "shutdown-timeout", 30*time.Second, "graceful shutdown timeout")
	// Storage flags
	fs.StringVar(&storageType, "storage", envOrDefault("STORAGE_TYPE", "local"), "storage backend: local, s3, or hybrid")
	fs.StringVar(&cacheDir, "dir", envOrDefault("LOCAL_DIR", "./cache-data"), "directory to store cache entries (local storage)")
	fs.StringVar(&s3Bucket, "s3-bucket", envOrDefault("S3_BUCKET", ""), "S3 bucket name")
	fs.StringVar(&s3Prefix, "s3-prefix", envOrDefault("S3_PREFIX", ""), "S3 key prefix")
	fs.StringVar(&s3Region, "s3-region", envOrDefault("S3_REGION", ""), "S3 region")
	fs.StringVar(&s3Endpoint, "s3-endpoint", envOrDefault("S3_ENDPOINT", ""), "S3 endpoint URL (for MinIO, etc.)")
	fs.IntVar(&s3Concurrency, "s3-concurrency", 0, "S3 upload concurrency (0 = default)")
	fs.IntVar(&s3RetryMax, "s3-retry-max", 3, "max retries for S3 operations")
	fs.Int64Var(&maxUpload, "max-upload", 0, "max upload size per cache entry in bytes (0 = unlimited)")
	// Auth flags
	fs.StringVar(&authUser, "auth-username", envOrDefault("AUTH_USERNAME", ""), "HTTP Basic authentication username (disabled when empty)")
	fs.StringVar(&authPass, "auth-password", envOrDefault("AUTH_PASSWORD", ""), "HTTP Basic authentication password (disabled when empty)")
	// Local cache flags
	fs.DurationVar(&localTTL, "local-ttl", localTTL, "local cache TTL based on last access time (e.g. 24h, 7d, 0=disabled)")
	fs.DurationVar(&localCleanupInterval, "local-cleanup-interval", localCleanupInterval, "interval between local cache cleanup runs (e.g. 1h, 1d)")
	// Rate limiting
	fs.Float64Var(&rateLimitPerIP, "rate-limit-per-ip", 0, "per-IP rate limit (requests per second, 0 = disabled)")
	fs.Float64Var(&rateLimitGlobal, "rate-limit-global", 0, "global rate limit (requests per second, 0 = disabled)")
	// Memory cache
	fs.Int64Var(&memCacheSize, "mem-cache-size", 0, "in-memory LRU cache size in bytes (0 = disabled)")
	fs.Int64Var(&memCacheMaxEntry, "mem-cache-max-entry", 65536, "max individual entry size to cache in memory")
	// Async S3 upload
	fs.BoolVar(&asyncS3Upload, "async-s3-upload", false, "enable async S3 upload in hybrid mode")
	fs.IntVar(&asyncS3QueueSize, "async-s3-queue-size", 1000, "max pending async S3 uploads")
	fs.IntVar(&asyncS3Workers, "async-s3-workers", 2, "number of async S3 upload workers")
	fs.IntVar(&asyncS3MaxRetry, "async-s3-max-retry", 3, "max retries for async S3 uploads")
	// Circuit breaker
	fs.IntVar(&circuitBreakerFailures, "circuit-breaker-failures", 0, "consecutive failures before opening circuit breaker (0 = disabled)")
	fs.DurationVar(&circuitBreakerTimeout, "circuit-breaker-timeout", 0, "circuit breaker cooldown duration (0 = disabled)")
	// TLS
	fs.StringVar(&tlsCert, "tls-cert", envOrDefault("TLS_CERT", ""), "TLS certificate file path")
	fs.StringVar(&tlsKey, "tls-key", envOrDefault("TLS_KEY", ""), "TLS key file path")

	// Parse env vars for flags that may not be overridden by command line
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
	if v := os.Getenv("REQUEST_TIMEOUT"); v != "" && requestTimeout == 30*time.Second {
		if d, err := time.ParseDuration(v); err == nil {
			requestTimeout = d
		}
	}
	if v := os.Getenv("SHUTDOWN_TIMEOUT"); v != "" && shutdownTimeout == 30*time.Second {
		if d, err := time.ParseDuration(v); err == nil {
			shutdownTimeout = d
		}
	}
	if v := os.Getenv("RATE_LIMIT_PER_IP"); v != "" && rateLimitPerIP == 0 {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			rateLimitPerIP = f
		}
	}
	if v := os.Getenv("RATE_LIMIT_GLOBAL"); v != "" && rateLimitGlobal == 0 {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			rateLimitGlobal = f
		}
	}
	if v := os.Getenv("MEM_CACHE_SIZE"); v != "" && memCacheSize == 0 {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			memCacheSize = n
		}
	}
	if v := os.Getenv("MEM_CACHE_MAX_ENTRY"); v != "" && memCacheMaxEntry == 65536 {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			memCacheMaxEntry = n
		}
	}
	if v := os.Getenv("CIRCUIT_BREAKER_FAILURES"); v != "" && circuitBreakerFailures == 0 {
		if n, err := strconv.Atoi(v); err == nil {
			circuitBreakerFailures = n
		}
	}
	if v := os.Getenv("CIRCUIT_BREAKER_TIMEOUT"); v != "" && circuitBreakerTimeout == 0 {
		if d, err := time.ParseDuration(v); err == nil {
			circuitBreakerTimeout = d
		}
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if showVersion {
		fmt.Println(version)
		return nil
	}

	// Setup structured logging
	logging.Setup(logging.Config{Format: logFormat, Level: logLevel})

	auth, err := newAuthConfig(authUser, authPass)
	if err != nil {
		return err
	}

	var backend storage.Backend
	var cleanup func() // optional cleanup function

	// Create health registry
	hRegistry := health.NewRegistry()
	hRegistry.Register(&health.LivenessChecker{})

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
		// Register local health check
		hRegistry.Register(health.TimeoutChecker(&health.LocalChecker{NameVal: "local", Dir: cacheDir}, 5*time.Second))
	case "s3":
		rawS3, err := storage.NewS3(ctx, storage.S3Options{
			Bucket:      s3Bucket,
			Prefix:      s3Prefix,
			Region:      s3Region,
			Endpoint:    s3Endpoint,
			Concurrency: s3Concurrency,
			RetryMax:    s3RetryMax,
		})
		if err != nil {
			return fmt.Errorf("failed to create S3 storage: %w", err)
		}
		backend = rawS3
		// Register S3 health check
		hRegistry.Register(health.TimeoutChecker(&health.S3Checker{
			NameVal: "s3",
			CheckFn: rawS3.CheckBucket,
		}, 10*time.Second))
	case "hybrid":
		if s3Bucket == "" {
			return fmt.Errorf("s3-bucket is required when storage type is hybrid")
		}
		localBackend, err := storage.NewLocal(cacheDir)
		if err != nil {
			return fmt.Errorf("failed to create local storage: %w", err)
		}
		if localTTL > 0 {
			localBackend.StartCleanup(ctx, localTTL, localCleanupInterval)
		}
		rawS3, err := storage.NewS3(ctx, storage.S3Options{
			Bucket:      s3Bucket,
			Prefix:      s3Prefix,
			Region:      s3Region,
			Endpoint:    s3Endpoint,
			Concurrency: s3Concurrency,
			RetryMax:    s3RetryMax,
		})
		if err != nil {
			return fmt.Errorf("failed to create S3 storage: %w", err)
		}
		s3Backend := storage.Backend(rawS3)
		// Wrap S3 with circuit breaker if configured
		if circuitBreakerFailures > 0 && circuitBreakerTimeout > 0 {
			s3Backend = storage.NewCircuitBreaker(rawS3, circuitBreakerFailures, circuitBreakerTimeout)
		}
		hybridOpts := storage.HybridOptions{
			AsyncUpload:    asyncS3Upload,
			AsyncQueueSize: asyncS3QueueSize,
			AsyncWorkers:   asyncS3Workers,
			AsyncMaxRetry:  asyncS3MaxRetry,
		}
		hybridBackend := storage.NewHybrid(localBackend, s3Backend, hybridOpts)
		if hybridOpts.AsyncUpload {
			cleanup = hybridBackend.StopAsyncUploader
		}
		backend = hybridBackend
		// Register health checks
		hRegistry.Register(health.TimeoutChecker(&health.LocalChecker{NameVal: "local", Dir: cacheDir}, 5*time.Second))
		hRegistry.Register(health.TimeoutChecker(&health.S3Checker{
			NameVal: "s3",
			CheckFn: rawS3.CheckBucket,
		}, 10*time.Second))
	default:
		return fmt.Errorf("unknown storage type: %s", storageType)
	}

	// Wrap backend with LRU cache if configured
	if memCacheSize > 0 {
		cached, err := storage.NewLRUCache(backend, memCacheSize, memCacheMaxEntry)
		if err != nil {
			return fmt.Errorf("failed to create in-memory cache: %w", err)
		}
		backend = cached
	}

	// Wrap backend with singleflight for deduplication
	backend = storage.NewSingleFlightBackend(backend)

	server := NewCacheServerWithAuth(backend, maxUpload, auth)
	server.health = hRegistry
	server.requestTimeout = requestTimeout

	mux := http.NewServeMux()
	mux.HandleFunc("/cache/", server.instrument("cache", server.requireAuth(server.handleCache)))
	mux.HandleFunc("/health", server.instrument("health", server.handleHealth))
	mux.HandleFunc("/livez", server.handleLivez)
	mux.HandleFunc("/readyz", server.handleReadyz)
	mux.HandleFunc("/version", server.handleVersion)
	mux.Handle("/metrics", server.requireAuth(metrics.Handler().ServeHTTP))

	// Rate limiting
	if rateLimitPerIP > 0 || rateLimitGlobal > 0 {
		rl := middleware.NewRateLimiter(rateLimitGlobal, rateLimitPerIP)
		server.rateLimiter = rl
	}

	var handler http.Handler = mux
	if server.rateLimiter != nil {
		handler = middleware.RateLimit(server.rateLimiter)(mux.ServeHTTP)
	}

	srv := &http.Server{
		Addr:         listenAddr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start debug server if configured
	var debugSrv *http.Server
	if debugListenAddr != "" {
		dmux := http.NewServeMux()
		dmux.HandleFunc("/debug/pprof/", pprof.Index)
		dmux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		dmux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		dmux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		dmux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		debugSrv = &http.Server{
			Addr:    debugListenAddr,
			Handler: dmux,
		}
		go func() {
			slog.Info("debug server listening", "addr", debugListenAddr)
			if err := debugSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("debug server error", "error", err)
			}
		}()
	}

	// Startup diagnostics
	slog.Info("starting cache server",
		"version", version,
		"listen", listenAddr,
		"storage", storageType,
		"log_format", logFormat,
		"log_level", logLevel,
		"request_timeout", requestTimeout,
		"shutdown_timeout", shutdownTimeout,
		"mem_cache_size", memCacheSize,
		"mem_cache_max_entry", memCacheMaxEntry,
		"rate_limit_per_ip", rateLimitPerIP,
		"rate_limit_global", rateLimitGlobal,
		"async_s3_upload", asyncS3Upload,
		"circuit_breaker_failures", circuitBreakerFailures,
		"circuit_breaker_timeout", circuitBreakerTimeout,
	)

	errCh := make(chan error, 1)
	go func() {
		if tlsCert != "" && tlsKey != "" {
			slog.Info("server listening with TLS", "addr", listenAddr)
			errCh <- srv.ListenAndServeTLS(tlsCert, tlsKey)
		} else {
			slog.Info("server listening", "addr", listenAddr)
			errCh <- srv.ListenAndServe()
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down gracefully", "reason", ctx.Err())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if cleanup != nil {
			cleanup()
		}
		if debugSrv != nil {
			_ = debugSrv.Shutdown(shutdownCtx)
		}
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
