# Changelog

All notable changes to this project will be documented in this file.

## [0.2.0] — 2025-06-30

### Phase 1 — Reliability

- **Async S3 Upload** — In hybrid mode, `PUT` returns immediately to the client after local write; S3 upload happens in the background via configurable worker pool and queue.
- **Circuit Breaker** — Automatic failover to local storage when S3 fails repeatedly. Configurable failure threshold and cooldown duration.
- **Rate Limiting** — Per-IP and global token-bucket rate limiting via `golang.org/x/time/rate`.
- **Singleflight Deduplication** — Concurrent identical requests collapse to a single backend call, preventing thundering herd.
- **Per-Request Timeouts** — Each request has a configurable timeout, preventing long-running S3 operations from blocking clients.
- **Graceful Shutdown** — On SIGTERM, the server waits for async S3 uploads to drain before exiting, with a configurable timeout.

### Phase 2 — Performance

- **In-Memory LRU Cache** — Optional RAM-based hot cache with configurable total size and per-entry max size. Uses `github.com/hashicorp/golang-lru/v2`.
- **Sharded File Locks** — 256 FNV-1a shards eliminate `sync.RWMutex` contention under high concurrency.
- **Buffer Pool** — `sync.Pool` reduces GC pressure by reusing file I/O buffers.
- **S3 Retry with Exponential Backoff** — Configurable max retries for S3 operations.
- **S3 Range Request Support** — Range headers are passed through to S3 for partial downloads.

### Phase 3 — Observability

- **Structured Logging** — Migrated to `log/slog` with JSON or text output. Configurable log level (`debug`, `info`, `warn`, `error`).
- **Request IDs** — `X-Request-ID` header propagation for distributed tracing.
- **Health Probes** — New endpoints: `/livez` (liveness), `/readyz` (readiness), `/version`.
- **Prometheus Metrics** — Added 8 new metrics: `s3_request_duration_seconds`, `local_cache_entries_total`, `local_cache_size_bytes`, `local_cleanup_runs_total`, `local_cleanup_evicted_bytes_total`, `circuit_breaker_state`, `rate_limit_hits_total`, `memory_cache_hits_total`, `memory_cache_misses_total`.
- **pprof Support** — Optional `-debug-listen` server exposes CPU, heap, goroutine, and trace profiles.
- **TLS Support** — `-tls-cert` and `-tls-key` flags for HTTPS termination.

### Helm Chart

- **Version bumped to 0.2.0** — Chart and app version aligned.
- **Deployment → StatefulSet** — Converted to StatefulSet with `volumeClaimTemplates` so each pod gets its own persistent PVC. This fixes the `ReadWriteOnce` multi-attach issue.
- **New Values** — Added 18+ new config values covering all Phase 1–3 features.
- **TLS Secret Template** — New `tls-secret.yaml` template for auto-creating TLS secrets from inline cert/key.
- **Debug Port Exposure** — Service template conditionally exposes the debug port when configured.
- **Probe Endpoints Updated** — Liveness and readiness probes now use `/livez` and `/readyz`.
- **Production Values** — Added `values-2gb.yaml` (2GB RAM pod config) and `values-eks.yaml` (EKS same-cluster optimized config).

### Testing

- **Gradle Cache Simulation** — Real Android project build test showing 2.75x speedup with hot cache.
- **ccache Simulation** — Simulated heavy C++ compilation showing 99x speedup with hot cache.
- **Cache Hit Rate** — 99% cache hit rate achieved with ccache + Android Automotive OS builds.

## [0.1.0] — 2024-XX-XX

- Initial release with local, S3, and hybrid storage backends.
- Prometheus metrics, HTTP Basic auth, Helm chart.
- Gradle and ccache client support.
