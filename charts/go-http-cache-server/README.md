# go-http-cache-server

Helm chart for deploying `go-http-cache-server`, a lightweight Gradle remote build cache server written in Go.

## Install

```bash
helm install go-http-cache-server oci://ghcr.io/akrumov/go-http-cache-server \
  --namespace gradle-cache \
  --create-namespace \
  --version 0.1.0
```

## Configuration

Common values:

| Value | Description | Default |
| --- | --- | --- |
| `image.repository` | Container image repository | `ghcr.io/akrumov/go-http-cache-server` |
| `image.tag` | Container image tag | `latest` |
| `replicaCount` | Replica count when autoscaling is disabled | `2` |
| `config.storageType` | Storage backend: `local`, `s3`, or `hybrid` | `s3` |
| `config.s3Bucket` | S3 bucket for cache objects | `my-gradle-cache` |
| `config.s3Region` | S3 region | `us-east-1` |
| `config.s3Concurrency` | S3 upload concurrency (`0` = SDK default of 5) | `0` |
| `config.localTTL` | Local cache TTL, e.g. `7d` (required for hybrid/local TTL) | `""` |
| `config.localCleanupInterval` | Interval between cleanups, e.g. `24h` | `""` |
| `secret.data.AUTH_USERNAME` | HTTP Basic authentication username | `""` |
| `secret.data.AUTH_PASSWORD` | HTTP Basic authentication password | `""` |
| `autoscaling.enabled` | Enable HorizontalPodAutoscaler | `true` |
| `persistence.enabled` | Enable PVC for local storage | `false` |
| `persistence.mountOptions` | Mount options for the cache volume; include `strictatime` for accurate TTL | `["strictatime"]` |
| `serviceAccount.annotations` | Service account annotations, including EKS IRSA | `{}` |
| `securityContext.runAsUser` | Numeric container user ID | `1000` |

Set both `secret.data.AUTH_USERNAME` and `secret.data.AUTH_PASSWORD` to require HTTP Basic authentication for `/cache/*` and `/metrics`. `/health` remains unauthenticated for probes.

## EKS IRSA

```bash
helm upgrade --install go-http-cache-server oci://ghcr.io/akrumov/go-http-cache-server \
  --namespace gradle-cache \
  --create-namespace \
  --version 0.1.0 \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::<ACCOUNT_ID>:role/GradleCacheS3Role \
  --set secret.create=false
```
