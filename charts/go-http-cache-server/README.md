# go-http-cache-server

Helm chart for deploying `go-http-cache-server`, a lightweight Gradle and ccache remote build cache server written in Go.

## Install

```bash
helm upgrade --install go-http-cache-server ./charts/go-http-cache-server \
  --namespace gradle-cache \
  --create-namespace \
  --set config.s3Bucket=my-gradle-cache \
  --set config.s3Region=us-east-1
```

After chart releases are published, install from OCI registry:

```bash
helm upgrade --install go-http-cache-server oci://ghcr.io/akrumov/go-http-cache-server \
  --namespace gradle-cache \
  --create-namespace \
  --version 0.2.0
```

## Configuration

### Core Values

| Value | Description | Default |
| --- | --- | --- |
| `image.repository` | Container image repository | `ghcr.io/akrumov/go-http-cache-server` |
| `image.tag` | Container image tag | `""` (defaults to `appVersion`) |
| `replicaCount` | Replica count when autoscaling is disabled | `2` |
| `config.storageType` | Storage backend: `local`, `s3`, or `hybrid` | `s3` |
| `config.s3Bucket` | S3 bucket for cache objects | `my-gradle-cache` |
| `config.s3Region` | S3 region | `us-east-1` |
| `config.s3Endpoint` | Custom S3 endpoint (MinIO, etc.) | `""` |
| `config.s3Concurrency` | S3 upload concurrency | `0` |
| `config.s3RetryMax` | Max retries for S3 operations | `3` |
| `config.localTTL` | Local cache TTL, e.g. `7d` | `""` |
| `config.localCleanupInterval` | Interval between cleanups, e.g. `24h` | `""` |
| `config.maxUploadSize` | Max upload size per entry in bytes | `""` |

### Reliability & Performance Values

| Value | Description | Default |
| --- | --- | --- |
| `config.requestTimeout` | Per-request operation timeout | `30s` |
| `config.shutdownTimeout` | Graceful shutdown timeout | `30s` |
| `config.logFormat` | Log format: `text` or `json` | `text` |
| `config.logLevel` | Log level: `debug`, `info`, `warn`, `error` | `info` |
| `config.memCacheSize` | In-memory LRU cache size in bytes (`0` = disabled) | `0` |
| `config.memCacheMaxEntry` | Max individual entry size for memory cache | `65536` |
| `config.rateLimitPerIP` | Per-IP rate limit (req/sec, `0` = disabled) | `0` |
| `config.rateLimitGlobal` | Global rate limit (req/sec, `0` = disabled) | `0` |
| `config.asyncS3Upload` | Enable async S3 upload in hybrid mode | `false` |
| `config.asyncS3QueueSize` | Max pending async uploads | `1000` |
| `config.asyncS3Workers` | Async S3 upload workers | `2` |
| `config.asyncS3MaxRetry` | Max retries for async uploads | `3` |
| `config.circuitBreakerFailures` | Consecutive failures before opening (`0` = disabled) | `0` |
| `config.circuitBreakerTimeout` | Circuit breaker cooldown, e.g. `30s` | `""` |
| `config.debugListen` | Debug/pprof server address (`:6060` or `""` = disabled) | `""` |

### Infrastructure Values

| Value | Description | Default |
| --- | --- | --- |
| `autoscaling.enabled` | Enable HorizontalPodAutoscaler | `true` |
| `autoscaling.minReplicas` | Minimum replicas | `2` |
| `autoscaling.maxReplicas` | Maximum replicas | `10` |
| `autoscaling.targetCPUUtilizationPercentage` | HPA CPU target | `70` |
| `persistence.enabled` | Enable per-pod PVC via StatefulSet | `false` |
| `persistence.size` | PVC storage per pod | `10Gi` |
| `persistence.storageClass` | Storage class (e.g. `gp3`, `efs`) | `""` |
| `persistence.accessModes` | PVC access modes | `["ReadWriteOnce"]` |
| `persistence.mountOptions` | Mount options; include `strictatime` for TTL | `["strictatime"]` |
| `service.type` | Service type | `ClusterIP` |
| `service.port` | Service port | `80` |
| `tls.enabled` | Enable TLS termination | `false` |
| `tls.existingSecret` | Existing secret containing `tls.crt` and `tls.key` | `""` |
| `tls.cert` | TLS certificate (inline, creates secret) | `""` |
| `tls.key` | TLS key (inline, creates secret) | `""` |
| `secret.data.AUTH_USERNAME` | HTTP Basic auth username | `""` |
| `secret.data.AUTH_PASSWORD` | HTTP Basic auth password | `""` |
| `serviceAccount.annotations` | Service account annotations (EKS IRSA) | `{}` |
| `resources.requests.memory` | Memory request | `128Mi` |
| `resources.limits.memory` | Memory limit | `1Gi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.limits.cpu` | CPU limit | `1000m` |

## Production Configurations

### 2GB RAM Pod (values-2gb.yaml)

Optimized for high-throughput builds with 2GB RAM per pod:

```yaml
config:
  storageType: hybrid
  s3Bucket: my-gradle-cache
  s3Region: us-east-1
  s3Concurrency: 4
  localTTL: 7d
  localCleanupInterval: 6h
  memCacheSize: 536870912      # 512 MB
  memCacheMaxEntry: 1048576    # 1 MB
  asyncS3Upload: true
  asyncS3QueueSize: 5000
  asyncS3Workers: 4
  circuitBreakerFailures: 5
  circuitBreakerTimeout: 30s
  rateLimitPerIP: 0             # disabled for high RPS
  rateLimitGlobal: 0
  logFormat: json
  logLevel: info

persistence:
  enabled: true
  storageClass: gp3
  size: 100Gi

autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 10

resources:
  requests:
    memory: 1536Mi
    cpu: 500m
  limits:
    memory: 2048Mi
    cpu: 2000m
```

Deploy:
```bash
helm upgrade --install cache ./charts/go-http-cache-server \
  -f ./charts/go-http-cache-server/values-2gb.yaml
```

### EKS Same-Cluster (values-eks.yaml)

Optimized for EKS where CI jobs and cache pods run in the same cluster with S3 VPC endpoint:

```yaml
config:
  storageType: hybrid
  s3Bucket: my-company-aaos-cache
  s3Prefix: ccache
  s3Region: us-east-1
  s3Endpoint: ""               # AWS S3 via VPC endpoint
  s3Concurrency: 16            # High concurrency from EKS
  s3RetryMax: 3
  localTTL: 14d
  localCleanupInterval: 6h
  memCacheSize: 1073741824     # 1 GB
  memCacheMaxEntry: 1048576
  maxUploadSize: 536870912     # 512 MB max entry
  rateLimitPerIP: 0            # disabled
  rateLimitGlobal: 0
  asyncS3Upload: true
  asyncS3QueueSize: 20000      # 20K queue for AAOS upload storms
  asyncS3Workers: 8
  asyncS3MaxRetry: 3
  circuitBreakerFailures: 20
  circuitBreakerTimeout: 60s
  logFormat: json
  logLevel: info

persistence:
  enabled: true
  storageClass: gp3            # EBS gp3 per pod
  size: 100Gi
  accessModes:
    - ReadWriteOnce

autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 20
  targetCPUUtilizationPercentage: 70

service:
  type: ClusterIP              # Internal DNS only; no Ingress/LoadBalancer
```

Deploy:
```bash
helm upgrade --install cache ./charts/go-http-cache-server \
  -f ./charts/go-http-cache-server/values-eks.yaml
```

## EKS IRSA

```bash
helm upgrade --install go-http-cache-server ./charts/go-http-cache-server \
  --namespace gradle-cache \
  --create-namespace \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::<ACCOUNT_ID>:role/GradleCacheS3Role \
  --set secret.create=false
```

## StatefulSet & Storage

The chart deploys as a **StatefulSet** with `volumeClaimTemplates`, which means:

- Each pod gets its own independent PVC (e.g., `cache-data-cache-server-0`, `cache-data-cache-server-1`)
- Storage **persists across pod restarts and version upgrades**
- `ReadWriteOnce` works correctly because each pod has its own volume
- Total storage = `replicaCount × persistence.size`

**Important:** If `helm uninstall` is run, PVCs are deleted by default. To protect cache data, add the `helm.sh/resource-policy: keep` annotation to the volumeClaimTemplates.

## Probes

The chart uses the new Kubernetes-native probes from v0.2.0:

| Probe | Endpoint | Purpose |
|-------|----------|---------|
| Liveness | `/livez` | Process is alive |
| Readiness | `/readyz` | All backends are healthy |

If a backend (S3, local disk) is down, `/readyz` returns 503, and the pod is removed from the service endpoints until it recovers.
