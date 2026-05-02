# Gradle Remote Build Cache Server

[![CI](https://github.com/yourusername/go-gradle-cache/actions/workflows/ci.yml/badge.svg)](https://github.com/yourusername/go-gradle-cache/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/yourusername/go-gradle-cache)](https://goreportcard.com/report/github.com/yourusername/go-gradle-cache)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A lightweight, high-performance remote build cache server for [Gradle](https://gradle.org/) written in Go. Supports both local filesystem and S3-compatible storage backends (AWS S3, MinIO, etc.). Includes Prometheus metrics, structured logging, and Kubernetes-ready deployment manifests.

## Features

- **Dual Storage Backends** — switch between local filesystem and S3 with a single flag
- **S3-Compatible** — works with AWS S3, MinIO, Wasabi, DigitalOcean Spaces, and more
- **Prometheus Metrics** — request counts, durations, cache hit/miss ratios, bytes stored/served
- **Graceful Shutdown** — handles SIGINT/SIGTERM properly
- **Small Docker Image** — multi-stage Alpine build (~15 MB)
- **Kubernetes Ready** — includes EKS deployment manifests with IRSA support
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

## Configuration

Every option can be set via **command-line flag** or **environment variable**. Flags take precedence over environment variables.

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `-listen` | — | `:8080` | Address to listen on |
| `-storage` | `STORAGE_TYPE` | `local` | Backend: `local` or `s3` |
| `-dir` | `LOCAL_DIR` | `./cache-data` | Local cache directory |
| `-s3-bucket` | `S3_BUCKET` | — | S3 bucket name |
| `-s3-prefix` | `S3_PREFIX` | — | S3 key prefix |
| `-s3-region` | `S3_REGION` | — | AWS region |
| `-s3-endpoint` | `S3_ENDPOINT` | — | Custom endpoint (MinIO, etc.) |
| `-max-upload` | `MAX_UPLOAD_SIZE` | `0` | Max upload size in bytes (`0` = unlimited) |
| `-version` | — | — | Print version and exit |

### Configuration Examples

**Via flags:**
```bash
./go-gradle-cache \
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

./go-gradle-cache
```

**Mixed (flags override env vars):**
```bash
export STORAGE_TYPE=s3
export S3_BUCKET=my-gradle-cache
export S3_REGION=us-east-1

# Uses the env vars above, but overrides the bucket
./go-gradle-cache -s3-bucket=another-bucket
```

### AWS Credentials

The server uses the standard AWS SDK credential chain:
1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
2. Shared credentials file (`~/.aws/credentials`)
3. IAM role (EC2, ECS, EKS/IRSA)

## Docker

```bash
docker build -t go-gradle-cache .

# Local storage
docker run -p 8080:8080 \
  -v $(pwd)/cache-data:/app/cache-data \
  go-gradle-cache \
  -storage=local -dir=/app/cache-data

# S3 storage
docker run -p 8080:8080 \
  -e AWS_ACCESS_KEY_ID=xxx \
  -e AWS_SECRET_ACCESS_KEY=yyy \
  -e STORAGE_TYPE=s3 \
  -e S3_BUCKET=my-gradle-cache \
  -e S3_REGION=us-east-1 \
  go-gradle-cache
```

## Kubernetes / EKS

Apply the included manifests:

```bash
kubectl apply -f k8s/eks-deployment.yaml
```

For production S3 access on EKS, use [IAM Roles for Service Accounts (IRSA)](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html):

1. Uncomment the `eks.amazonaws.com/role-arn` annotation in `k8s/eks-deployment.yaml`
2. Remove the `Secret` and `secretRef` from the Deployment
3. Apply the manifest

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
    }
}
```

Enable caching in `gradle.properties`:

```properties
org.gradle.caching=true
```

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
