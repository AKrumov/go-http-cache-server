# go-gradle-cache

Helm chart for deploying `go-gradle-cache`, a lightweight Gradle remote build cache server written in Go.

## Install

```bash
helm install go-gradle-cache oci://ghcr.io/akrumov/go-gradle-cache \
  --namespace gradle-cache \
  --create-namespace \
  --version 0.1.0
```

## Configuration

Common values:

| Value | Description | Default |
| --- | --- | --- |
| `image.repository` | Container image repository | `ghcr.io/akrumov/go-gradle-cache` |
| `image.tag` | Container image tag | `latest` |
| `replicaCount` | Replica count when autoscaling is disabled | `2` |
| `config.storageType` | Storage backend, `local` or `s3` | `s3` |
| `config.s3Bucket` | S3 bucket for cache objects | `my-gradle-cache` |
| `config.s3Region` | S3 region | `us-east-1` |
| `secret.data.AUTH_USERNAME` | HTTP Basic authentication username | `""` |
| `secret.data.AUTH_PASSWORD` | HTTP Basic authentication password | `""` |
| `autoscaling.enabled` | Enable HorizontalPodAutoscaler | `true` |
| `persistence.enabled` | Enable PVC for local storage | `false` |
| `serviceAccount.annotations` | Service account annotations, including EKS IRSA | `{}` |
| `securityContext.runAsUser` | Numeric container user ID | `1000` |

Set both `secret.data.AUTH_USERNAME` and `secret.data.AUTH_PASSWORD` to require HTTP Basic authentication for `/cache/*` and `/metrics`. `/health` remains unauthenticated for probes.

## EKS IRSA

```bash
helm upgrade --install go-gradle-cache oci://ghcr.io/akrumov/go-gradle-cache \
  --namespace gradle-cache \
  --create-namespace \
  --version 0.1.0 \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::<ACCOUNT_ID>:role/GradleCacheS3Role \
  --set secret.create=false
```
