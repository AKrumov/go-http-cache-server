# Gradle Remote Build Cache Server

[![CI](https://github.com/AKrumov/go-http-cache-server/actions/workflows/ci.yml/badge.svg)](https://github.com/AKrumov/go-http-cache-server/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/AKrumov/go-http-cache-server)](https://goreportcard.com/report/github.com/AKrumov/go-http-cache-server)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A lightweight, high-performance remote build cache server for [Gradle](https://gradle.org/) written in Go. Supports local filesystem, S3-compatible storage (AWS S3, MinIO, etc.), and a hybrid mode that caches locally while backing everything with S3. Includes Prometheus metrics, structured logging, and a Kubernetes-ready Helm chart.

## Features

- **Multiple Storage Backends** — local filesystem, S3, or a hybrid local+S3 tiered cache
- **S3-Compatible** — works with AWS S3, MinIO, Wasabi, DigitalOcean Spaces, and more
- **Prometheus Metrics** — request counts, durations, cache hit/miss ratios, bytes stored/served
- **Graceful Shutdown** — handles SIGINT/SIGTERM properly
- **Small Docker Image** — multi-stage Alpine build (~15 MB)
- **Kubernetes Ready** — includes a Helm chart with EKS IRSA support
- **Secure by Default** — non-root container user, configurable request limits

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

In hybrid mode the server serves cache entries from the local filesystem when possible. If an entry is missing locally, it is downloaded from S3, stored locally, and then served. On PUT, entries are persisted to both local storage and S3.

```bash
go run ./app \
  -storage=hybrid \
  -dir=./cache-data \
  -s3-bucket=my-gradle-cache \
  -s3-region=us-east-1 \
  -local-ttl=7d \
  -local-cleanup-interval=24h
```

## Configuration

Every option can be set via **command-line flag** or **environment variable**. Flags take precedence over environment variables.

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `-listen` | — | `:8080` | Address to listen on |
| `-storage` | `STORAGE_TYPE` | `local` | Backend: `local`, `s3`, or `hybrid` |
| `-dir` | `LOCAL_DIR` | `./cache-data` | Local cache directory |
| `-local-ttl` | `LOCAL_TTL` | `0` | Local cache TTL based on last access time (`0` = disabled, e.g. `24h`, `7d`) |
| `-local-cleanup-interval` | `LOCAL_CLEANUP_INTERVAL` | `24h` | Interval between local cache cleanup runs (e.g. `1h`, `1d`) |
| `-s3-bucket` | `S3_BUCKET` | — | S3 bucket name |
| `-s3-prefix` | `S3_PREFIX` | — | S3 key prefix |
| `-s3-region` | `S3_REGION` | — | AWS region |
| `-s3-endpoint` | `S3_ENDPOINT` | — | Custom endpoint (MinIO, etc.) |
| `-s3-concurrency` | `S3_CONCURRENCY` | `0` | S3 upload concurrency (`0` = SDK default of 5) |
| `-max-upload` | `MAX_UPLOAD_SIZE` | `0` | Max upload size in bytes (`0` = unlimited) |
| `-auth-username` | `AUTH_USERNAME` | — | HTTP Basic authentication username |
| `-auth-password` | `AUTH_PASSWORD` | — | HTTP Basic authentication password |
| `-version` | — | — | Print version and exit |

### Configuration Examples

**Via flags:**
```bash
./go-http-cache-server \
  -listen=:8080 \
  -storage=s3 \
  -s3-bucket=my-gradle-cache \
  -s3-region=us-east-1 \
  -max-upload=10737418240
```

**Via environment variables:**
```bash
export STORAGE_TYPE=s3
export S3_BUCKET=my-gradle-cache
export S3_REGION=us-east-1
export MAX_UPLOAD_SIZE=10737418240
export AWS_ACCESS_KEY_ID=AKIA...
export AWS_SECRET_ACCESS_KEY=...

./go-http-cache-server
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

# Uses the env vars above, but overrides the bucket
./go-http-cache-server -s3-bucket=another-bucket
```

### Local Cache TTL / Eviction

When `local` or `hybrid` storage is used, you can enable automatic eviction of local cache entries that have not been accessed for a configured TTL. A background job runs at `-local-cleanup-interval` (default once per day) and deletes stale files immediately. There is no trash/archive folder.

```bash
go run ./app \
  -storage=hybrid \
  -dir=./cache-data \
  -s3-bucket=my-gradle-cache \
  -s3-region=us-east-1 \
  -local-ttl=7d
```

**Important — access time (`atime`) mount option:**

The TTL check relies on the file's last access time. Many Linux distributions and containers mount filesystems with `relatime` or `noatime` for performance. For accurate eviction, mount the cache directory with `strictatime`:

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

### HTTP Authentication

HTTP Basic authentication is disabled by default. Set both `AUTH_USERNAME` and `AUTH_PASSWORD` (or both matching flags) to require credentials for `/cache/*` and `/metrics`. The `/health` endpoint remains unauthenticated for load balancer and Kubernetes probes.

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

# Hybrid storage
docker run -p 8080:8080 \
  -v $(pwd)/cache-data:/app/cache-data \
  -e AWS_ACCESS_KEY_ID=xxx \
  -e AWS_SECRET_ACCESS_KEY=yyy \
  -e STORAGE_TYPE=hybrid \
  -e LOCAL_DIR=/app/cache-data \
  -e S3_BUCKET=my-gradle-cache \
  -e S3_REGION=us-east-1 \
  go-http-cache-server
```

## Kubernetes / EKS

Install the included Helm chart:

```bash
helm upgrade --install go-http-cache-server ./charts/go-http-cache-server \
  --namespace gradle-cache \
  --create-namespace \
  --set image.repository=ghcr.io/akrumov/go-http-cache-server \
  --set config.s3Bucket=my-gradle-cache \
  --set config.s3Region=us-east-1
```

After chart releases are published, install directly from the OCI registry:

```bash
helm upgrade --install go-http-cache-server oci://ghcr.io/akrumov/go-http-cache-server \
  --namespace gradle-cache \
  --create-namespace \
  --version 0.1.0
```

The chart is also discoverable on [Artifact Hub](https://artifacthub.io). To add it there, register the OCI repository:

- Kind: `Helm charts`
- Name: `go-http-cache-server`
- URL: `oci://ghcr.io/akrumov`

For verified publisher status, claim the repository on Artifact Hub and add your repository ID as the `ARTIFACTHUB_REPOSITORY_ID` GitHub Actions secret.

For production S3 access on EKS, use [IAM Roles for Service Accounts (IRSA)](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html):

```bash
helm upgrade --install go-http-cache-server ./charts/go-http-cache-server \
  --namespace gradle-cache \
  --create-namespace \
  --set image.repository=ghcr.io/akrumov/go-http-cache-server \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::<ACCOUNT_ID>:role/GradleCacheS3Role \
  --set secret.create=false
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

### Tuning S3 Upload Concurrency

The S3 backend uses the AWS SDK transfer manager to upload objects. By default the SDK uses a concurrency of **5** parallel part workers. You can override this with the `-s3-concurrency` flag (or `S3_CONCURRENCY` environment variable).

#### How to choose the right value

The main constraints are **memory** and **network bandwidth**.

**Memory bound**

The uploader allocates a buffer per concurrent part (default part size is 5 MB):

```
memory_per_active_upload = concurrency × 5 MB
memory_total             = memory_per_active_upload × simultaneous_uploads
```

For example, with `concurrency = 15` and **10 concurrent uploads** at peak:
- Upload buffers alone: `15 × 5 MB × 10 = 750 MB`
- Add Go runtime, HTTP buffers, and GC headroom: **~1.3 GB total**

**Recommended EKS resource limits for concurrency = 15**

When running 2–3 pods for 80+ Android applications:

```yaml
resources:
  requests:
    memory: "2Gi"
    cpu: "1000m"
  limits:
    memory: "4Gi"
    cpu: "2000m"
```

- **Memory limit of 4 GiB** handles bursts when many Gradle tasks flush cache artifacts simultaneously.
- **CPU limit of 2 cores** is sufficient because S3 uploads are I/O-bound, but TLS handshakes and many goroutines need scheduling headroom.

**Rule of thumb**

| Scenario | Recommended concurrency |
|----------|------------------------|
| Small objects (< 5 MB) | Doesn't matter; uploader falls back to single `PutObject` |
| Large objects, 1 Gbps same-region | 5–10 |
| Large objects, 10 Gbps or cross-region | 10–20 |
| Many parallel uploads (20+) per pod | Keep at 5–10 to avoid memory pressure |

Start with the default (5), measure latency and memory under load, and increase only if you have headroom.

## Metrics

Prometheus metrics are exposed at `/metrics`:

- `gradle_cache_requests_total` — HTTP requests by method, handler, status
- `gradle_cache_request_duration_seconds` — Request latency histogram
- `gradle_cache_hits_total` — Cache hits
- `gradle_cache_misses_total` — Cache misses
- `gradle_cache_entries_stored_total` — Entries successfully stored
- `gradle_cache_stored_bytes_total` — Bytes stored
- `gradle_cache_served_bytes_total` — Bytes served
- `gradle_cache_in_flight_requests` — Current active requests

## Development

```bash
# Build
make build

# Test
make test

# Lint
make lint

# Run locally
make run

# Build Docker image
make docker
```

## Architecture

```
┌─────────────┐     ┌──────────────────────┐     ┌─────────────────┐
│   Gradle    │────▶│  Cache Server (Go)   │────▶│  Local FS / S3  │
│   Client    │     │  - HTTP handlers     │     │  - Storage      │
│             │◀────│  - Prometheus metrics│◀────│  - Backend      │
└─────────────┘     └──────────────────────┘     └─────────────────┘
```

## License

MIT License — see [LICENSE](LICENSE) for details.
