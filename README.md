# Remote Build Cache Server for Gradle and ccache

[![CI](https://github.com/AKrumov/go-http-cache-server/actions/workflows/ci.yml/badge.svg)](https://github.com/AKrumov/go-http-cache-server/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/AKrumov/go-http-cache-server)](https://goreportcard.com/report/github.com/AKrumov/go-http-cache-server)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A lightweight, high-performance remote build cache server for [Gradle](https://gradle.org/) and [ccache](https://ccache.dev/) written in Go. Supports local filesystem, S3-compatible storage (AWS S3, MinIO, etc.), and a hybrid mode that caches locally while backing everything with S3. Includes Prometheus metrics, structured logging, circuit breaker, rate limiting, in-memory LRU cache, and a Kubernetes-ready Helm chart.

## Features

### Storage
- **Multiple Storage Backends** — local filesystem, S3, or hybrid local+S3 tiered cache
- **S3-Compatible** — works with AWS S3, MinIO, Wasabi, DigitalOcean Spaces, and more
- **Async S3 Upload** — hybrid mode returns immediately to client; uploads to S3 in background
- **S3 Retry & Range Requests** — automatic retries with exponential backoff; range header passthrough for partial downloads

### Reliability (Phase 1)
- **Circuit Breaker** — automatic fallback to local storage when S3 is degraded
- **Rate Limiting** — per-IP and global request throttling via token bucket
- **Singleflight Deduplication** — concurrent identical requests collapse to one backend call
- **Per-Request Timeouts** — prevents long-running operations from blocking clients
- **Graceful Shutdown** — handles SIGINT/SIGTERM with configurable timeout; drains async upload queue

### Performance (Phase 2)
- **Sharded File Locks** — 256 FNV-1a shards eliminate lock contention under high concurrency
- **In-Memory LRU Cache** — optional RAM-based cache for hot entries; configurable size and entry limits
- **Buffer Pool** — `sync.Pool` reduces GC pressure on file I/O
- **Concurrent S3 Uploads** — configurable concurrency with transfer manager

### Observability (Phase 3)
- **Structured Logging** — `log/slog` with JSON or text output; configurable log level
- **Request IDs** — `X-Request-ID` propagation for distributed tracing
- **Health Probes** — `/livez` (liveness), `/readyz` (readiness), `/health` (legacy), `/version`
- **Prometheus Metrics** — 12+ metrics including cache hits/misses, request latency, S3 duration, circuit breaker state, memory cache stats, rate limit hits
- **pprof Support** — optional debug server with CPU, heap, goroutine, and trace profiles
- **Small Docker Image** — multi-stage Alpine build (~15 MB)
- **Kubernetes Ready** — Helm chart with EKS IRSA support, StatefulSet for per-pod storage, autoscaling

## Quick Start

### Local (Filesystem)

```bash
go run ./app -storage=local -dir=./cache-data
```

### S3 (AWS)

```bash
go run ./app \
  -storage=s3 \
  -s3-bucket=my-gradle-cache \
  -s3-region=us-east-1
```

### MinIO

```bash
go run ./app \
  -storage=s3 \
  -s3-bucket=gradle-cache \
  -s3-endpoint=http://localhost:9000 \
  -s3-region=us-east-1
```

### Hybrid (Local + S3)

In hybrid mode the server serves cache entries from the local filesystem when possible. If an entry is missing locally, it is downloaded from S3, stored locally, and then served. On PUT, entries are written locally and returned to the client immediately; S3 upload happens asynchronously in the background.

```bash
go run ./app \
  -storage=hybrid \
  -dir=./cache-data \
  -s3-bucket=my-gradle-cache \
  -s3-region=us-east-1 \
  -local-ttl=7d \
  -local-cleanup-interval=24h \
  -async-s3-upload \
  -async-s3-workers=4
```

## Configuration

Every option can be set via **command-line flag** or **environment variable**. Flags take precedence over environment variables.

### Core Flags

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `-listen` | — | `:8080` | Address to listen on |
| `-storage` | `STORAGE_TYPE` | `local` | Backend: `local`, `s3`, or `hybrid` |
| `-dir` | `LOCAL_DIR` | `./cache-data` | Local cache directory |
| `-local-ttl` | `LOCAL_TTL` | `0` | Local cache TTL (`0` = disabled, e.g. `24h`, `7d`) |
| `-local-cleanup-interval` | `LOCAL_CLEANUP_INTERVAL` | `24h` | Interval between cleanups (e.g. `1h`, `1d`) |
| `-s3-bucket` | `S3_BUCKET` | — | S3 bucket name |
| `-s3-prefix` | `S3_PREFIX` | — | S3 key prefix |
| `-s3-region` | `S3_REGION` | — | AWS region |
| `-s3-endpoint` | `S3_ENDPOINT` | — | Custom endpoint (MinIO, etc.) |
| `-s3-concurrency` | `S3_CONCURRENCY` | `0` | S3 upload concurrency (`0` = SDK default of 5) |
| `-s3-retry-max` | `S3_RETRY_MAX` | `3` | Max retries for S3 operations |
| `-max-upload` | `MAX_UPLOAD_SIZE` | `0` | Max upload size in bytes (`0` = unlimited) |
| `-auth-username` | `AUTH_USERNAME` | — | HTTP Basic authentication username |
| `-auth-password` | `AUTH_PASSWORD` | — | HTTP Basic authentication password |
| `-version` | — | — | Print version and exit |

### Reliability & Performance Flags

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `-request-timeout` | `REQUEST_TIMEOUT` | `30s` | Per-request operation timeout |
| `-shutdown-timeout` | `SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown timeout |
| `-rate-limit-per-ip` | `RATE_LIMIT_PER_IP` | `0` | Per-IP rate limit (req/sec, `0` = disabled) |
| `-rate-limit-global` | `RATE_LIMIT_GLOBAL` | `0` | Global rate limit (req/sec, `0` = disabled) |
| `-mem-cache-size` | `MEM_CACHE_SIZE` | `0` | In-memory LRU cache size in bytes (`0` = disabled) |
| `-mem-cache-max-entry` | `MEM_CACHE_MAX_ENTRY` | `65536` | Max individual entry size for memory cache |
| `-async-s3-upload` | `ASYNC_S3_UPLOAD` | `false` | Enable async S3 upload in hybrid mode |
| `-async-s3-queue-size` | `ASYNC_S3_QUEUE_SIZE` | `1000` | Max pending async uploads |
| `-async-s3-workers` | `ASYNC_S3_WORKERS` | `2` | Async S3 upload workers |
| `-async-s3-max-retry` | `ASYNC_S3_MAX_RETRY` | `3` | Max retries for async uploads |
| `-circuit-breaker-failures` | `CIRCUIT_BREAKER_FAILURES` | `0` | Consecutive failures before opening (`0` = disabled) |
| `-circuit-breaker-timeout` | `CIRCUIT_BREAKER_TIMEOUT` | `0` | Circuit breaker cooldown (`0` = disabled) |
| `-debug-listen` | `DEBUG_LISTEN` | `""` | Debug/pprof server address (`:6060` or `""` = disabled) |
| `-tls-cert` | `TLS_CERT` | `""` | TLS certificate file path |
| `-tls-key` | `TLS_KEY` | `""` | TLS key file path |
| `-log-format` | `LOG_FORMAT` | `text` | Log format: `text` or `json` |
| `-log-level` | `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

### Configuration Examples

**Production hybrid with all features:**
```bash
./go-http-cache-server \
  -listen=:8080 \
  -storage=hybrid \
  -dir=/app/cache-data \
  -s3-bucket=my-gradle-cache \
  -s3-region=us-east-1 \
  -local-ttl=14d \
  -local-cleanup-interval=6h \
  -mem-cache-size=536870912 \
  -mem-cache-max-entry=1048576 \
  -async-s3-upload \
  -async-s3-workers=4 \
  -async-s3-queue-size=5000 \
  -circuit-breaker-failures=5 \
  -circuit-breaker-timeout=30s \
  -rate-limit-per-ip=100 \
  -rate-limit-global=1000 \
  -log-format=json \
  -log-level=info
```

**With HTTP Basic authentication:**
```bash
./go-http-cache-server \
  -storage=local \
  -dir=./cache-data \
  -auth-username=gradle \
  -auth-password=change-me
```

**Mixed (flags override env vars):**
```bash
export STORAGE_TYPE=s3
export S3_BUCKET=my-gradle-cache
export S3_REGION=us-east-1

# Uses env vars but overrides the bucket
./go-http-cache-server -s3-bucket=another-bucket
```

### Local Cache TTL / Eviction

When `local` or `hybrid` storage is used, you can enable automatic eviction of local cache entries that have not been accessed for a configured TTL. A background job runs at `-local-cleanup-interval` and deletes stale files immediately. There is no trash/archive folder.

```bash
go run ./app \
  -storage=hybrid \
  -dir=./cache-data \
  -s3-bucket=my-gradle-cache \
  -s3-region=us-east-1 \
  -local-ttl=7d
```

**Important — access time (`atime`) mount option:**

The TTL check relies on the file's last access time. Many Linux distributions and containers mount filesystems with `relatime` or `noatime`. For accurate eviction, mount the cache directory with `strictatime`:

```bash
mount -o remount,strictatime /path/to/cache-data
```

In Kubernetes, use `mountOptions` on the PV/PVC:

```yaml
mountOptions:
  - strictatime
```

If `noatime` is in effect, files will appear never to have been accessed and may all be evicted on the first cleanup run.

### AWS Credentials

The server uses the standard AWS SDK credential chain:
1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
2. Shared credentials file (`~/.aws/credentials`)
3. IAM role (EC2, ECS, EKS/IRSA)

## Docker

```bash
docker build -t go-http-cache-server .

# Local storage
docker run -p 8080:8080 \
  -v $(pwd)/cache-data:/app/cache-data \
  go-http-cache-server \
  -storage=local -dir=/app/cache-data

# S3 storage
docker run -p 8080:8080 \
  -e AWS_ACCESS_KEY_ID=xxx \
  -e AWS_SECRET_ACCESS_KEY=yyy \
  -e STORAGE_TYPE=s3 \
  -e S3_BUCKET=my-gradle-cache \
  -e S3_REGION=us-east-1 \
  go-http-cache-server

# Hybrid storage with async S3
docker run -p 8080:8080 \
  -v $(pwd)/cache-data:/app/cache-data \
  -e AWS_ACCESS_KEY_ID=xxx \
  -e AWS_SECRET_ACCESS_KEY=yyy \
  -e STORAGE_TYPE=hybrid \
  -e LOCAL_DIR=/app/cache-data \
  -e S3_BUCKET=my-gradle-cache \
  -e S3_REGION=us-east-1 \
  -e ASYNC_S3_UPLOAD=true \
  -e ASYNC_S3_WORKERS=4 \
  go-http-cache-server
```

## Kubernetes / EKS

Install the included Helm chart:

```bash
helm upgrade --install go-http-cache-server ./charts/go-http-cache-server \
  --namespace gradle-cache \
  --create-namespace \
  --set config.s3Bucket=my-gradle-cache \
  --set config.s3Region=us-east-1
```

For production S3 access on EKS, use [IAM Roles for Service Accounts (IRSA)](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html):

```bash
helm upgrade --install go-http-cache-server ./charts/go-http-cache-server \
  --namespace gradle-cache \
  --create-namespace \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::<ACCOUNT_ID>:role/GradleCacheS3Role \
  --set secret.create=false
```

### Production Values for 2GB RAM Pods

Use the provided `values-2gb.yaml` for a production-ready 2GB pod:

```yaml
config:
  storageType: hybrid
  s3Bucket: my-gradle-cache
  s3Region: us-east-1
  s3Concurrency: 16
  localTTL: 14d
  localCleanupInterval: 6h
  memCacheSize: 1073741824    # 1 GB
  memCacheMaxEntry: 1048576   # 1 MB
  asyncS3Upload: true
  asyncS3QueueSize: 10000
  asyncS3Workers: 8
  circuitBreakerFailures: 20
  circuitBreakerTimeout: 60s
  logFormat: json
  logLevel: info
  rateLimitPerIP: 0           # disabled for high-RPS environments
  rateLimitGlobal: 0

persistence:
  enabled: true
  storageClass: gp3
  size: 100Gi

autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 20
```

Deploy with:
```bash
helm upgrade --install go-http-cache-server ./charts/go-http-cache-server \
  -f ./charts/go-http-cache-server/values-2gb.yaml
```

### EKS Same-Cluster Optimized

When CI jobs and cache pods run in the same EKS cluster, use `values-eks.yaml` for optimized S3 VPC endpoint access, ClusterIP service, and per-pod EBS gp3 storage:

```bash
helm upgrade --install go-http-cache-server ./charts/go-http-cache-server \
  -f ./charts/go-http-cache-server/values-eks.yaml
```

## Gradle Client Setup

Add to your project's `settings.gradle.kts` (or `settings.gradle`):

```kotlin
buildCache {
    local {
        isEnabled = true
    }
    remote<HttpBuildCache> {
        url = uri("http://localhost:8080/cache/myapp")
        isEnabled = true
        isPush = providers.environmentVariable("CI").isPresent
        isAllowInsecureProtocol = true // only for local testing without HTTPS
        credentials {
            username = providers.environmentVariable("GRADLE_CACHE_USERNAME").orElse("").get()
            password = providers.environmentVariable("GRADLE_CACHE_PASSWORD").orElse("").get()
        }
    }
}
```

Enable caching in `gradle.properties`:

```properties
org.gradle.caching=true
```

### Gradle Cache Performance

In our tests with a real Android project, enabling the remote build cache showed:

| Scenario | Time | Speedup |
|----------|------|---------|
| No cache | 6.76s | 1.0x |
| Cold cache | 7.32s | 0.92x |
| Hot cache | 2.45s | **2.75x** |

## ccache Client Setup

Add to your `ccache.conf`:

```ini
remote_storage = http|url=http://localhost:8080/cache/ccache
```

With HTTP Basic authentication:

```ini
remote_storage = http|url=http://localhost:8080/cache/ccache|credentials=gradle:change-me
```

Or set via environment variable:

```bash
export CCACHE_REMOTE_STORAGE="http|url=http://localhost:8080/cache/ccache"
```

For Android Automotive OS (AAOS) builds with 99% cache hit rate, add:

```bash
export CCACHE_NODIRECT=true   # Prevents local source check; faster remote-only cache
export CCACHE_MAXSIZE=20G
```

### ccache Performance

With a simulated heavy C++ compilation workload:

| Scenario | Time | Speedup |
|----------|------|---------|
| No cache | 0.987s | 1.0x |
| Cold cache | 1.002s | 0.98x |
| Hot cache | 0.010s | **99x** |

## Performance Expectations

### Per-Pod RPS (Single 2GB Pod)

| Scenario | Approximate RPS |
|----------|----------------|
| Cache hit (in-memory LRU) | 20,000–50,000+ |
| Cache hit (local disk) | 5,000–15,000 |
| Cache hit (S3) | 500–2,000 |
| Cache miss (async hybrid) | 5,000–10,000 |
| Cache miss (sync S3) | 500–2,000 |

### Realistic Gradle Cache Workload (60% HEAD, 30% GET, 10% PUT)

A single pod with hybrid storage and 512MB LRU cache comfortably handles **5,000–10,000 RPS**. With 5–10 pods, you get **50,000+ RPS** total.

### Memory Budget (2GB Pod)

| Component | Allocation |
|-----------|-----------|
| In-memory LRU cache | 512 MB (25%) or 1 GB (50%) |
| Go runtime + overhead | ~200 MB |
| Async S3 queue buffers | ~100 MB |
| Local cache (OS page cache) | ~200 MB |
| Headroom | ~384 MB |

### S3 Concurrency Tuning

The S3 backend uses the AWS SDK transfer manager. By default, concurrency is **5**.

**Memory bound:**

```
memory_per_active_upload = concurrency × 5 MB
memory_total             = memory_per_active_upload × simultaneous_uploads
```

With `concurrency = 15` and **10 concurrent uploads** at peak:
- Upload buffers: `15 × 5 MB × 10 = 750 MB`
- Total with Go runtime: **~1.3 GB**

**Recommended concurrency:**

| Scenario | Recommended concurrency |
|----------|------------------------|
| Small objects (< 5 MB) | Doesn't matter; falls back to single `PutObject` |
| Large objects, 1 Gbps same-region | 5–10 |
| Large objects, 10 Gbps or cross-region | 10–20 |
| Many parallel uploads (20+) per pod | Keep at 5–10 to avoid memory pressure |

## Metrics

Prometheus metrics are exposed at `/metrics`:

- `gradle_cache_requests_total` — HTTP requests by method, handler, status
- `gradle_cache_request_duration_seconds` — Request latency histogram
- `gradle_cache_hits_total` — Cache hits by cache ID
- `gradle_cache_misses_total` — Cache misses by cache ID
- `gradle_cache_entries_stored_total` — Entries successfully stored
- `gradle_cache_stored_bytes_total` — Bytes stored
- `gradle_cache_served_bytes_total` — Bytes served
- `gradle_cache_in_flight_requests` — Current active requests
- `s3_request_duration_seconds` — S3 operation latency
- `local_cache_entries_total` — Local cache entries count
- `local_cache_size_bytes` — Local cache size
- `local_cleanup_runs_total` — Cleanup runs
- `local_cleanup_evicted_bytes_total` — Bytes evicted during cleanup
- `circuit_breaker_state` — Circuit breaker state (0=closed, 1=open, 2=half-open)
- `rate_limit_hits_total` — Rate limit triggered events
- `memory_cache_hits_total` — In-memory LRU cache hits
- `memory_cache_misses_total` — In-memory LRU cache misses

**Health probes:**
- `/livez` — Liveness probe (returns 200 if process is alive)
- `/readyz` — Readiness probe (returns 200 if all backends are healthy)
- `/health` — Legacy health endpoint (returns 200 if healthy)
- `/version` — Returns server version

## Architecture

```
                          ┌──────────────────────────────┐
                          │      Cache Server (Go)       │
                          │                              │
┌─────────────┐           │  ┌────────────────────────┐  │     ┌─────────────────┐
│   Build     │──────────▶│  │  Rate Limiting         │  │     │  Local FS       │
│   Client    │  HEAD/GET │  │  (per-IP / global)     │  │◀───▶│  (fast cache)   │
│             │  PUT      │  └────────────────────────┘  │     └─────────────────┘
└─────────────┘           │           │                      │              │
                          │           ▼                      │              │
                          │  ┌────────────────────────┐  │              │
                          │  │  In-Memory LRU Cache   │  │              │
                          │  │  (hot entries in RAM)  │  │              │
                          │  └────────────────────────┘  │              │
                          │           │                      │              │
                          │           ▼                      │              │
                          │  ┌────────────────────────┐  │              │
                          │  │  Singleflight          │  │              │
                          │  │  (deduplicate requests)│  │              │
                          │  └────────────────────────┘  │              │
                          │           │                      │              │
                          │           ▼                      │              │
                          │  ┌────────────────────────┐  │              │
                          │  │  Circuit Breaker       │  │              │
                          │  │  (failover to local)   │  │              │
                          │  └────────────────────────┘  │              │
                          │           │                      │              │
                          │     ┌─────┴─────┐                │              │
                          │     │           │                │              │
                          │     ▼           ▼                │              │
                          │  ┌──────┐  ┌──────────┐          │              │
                          │  │Local │  │   S3     │◀─────────┘              │
                          │  │  FS  │  │(durable) │◀────────────────────────┘
                          │  └──────┘  └──────────┘
                          │     │              │
                          │     │              ▼
                          │     │       ┌──────────────────┐
                          │     │       │ Async Upload     │
                          │     │       │ Queue + Workers  │
                          │     │       └──────────────────┘
                          │     │
                          │     ▼
                          │  ┌──────────────────┐
                          │  │ Prometheus       │
                          │  │ /metrics /pprof  │
                          │  └──────────────────┘
                          └──────────────────────────────┘
```

## Development

```bash
# Build
make build

# Test
make test

# Test with coverage
make test-coverage

# Lint
make lint

# Run locally
make run

# Build Docker image
make docker
```

## License

MIT License — see [LICENSE](LICENSE) for details.
