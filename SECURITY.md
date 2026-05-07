# Security Policy

## Supported Versions

We release security patches for the most recent release series. Please upgrade to the latest version before reporting an issue to ensure it has not already been addressed.

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| < latest| :x:                |

## Reporting a Vulnerability

If you discover a security vulnerability in this project, please report it responsibly.

**Please do not open a public issue or discussion.**

Instead, send an email to the maintainers with the following details:

- A description of the vulnerability
- Steps to reproduce the issue
- Possible impact
- Any suggested fixes or mitigations

We aim to acknowledge receipt within **48 hours** and will provide a timeline for a fix within **5 business days**. We ask that you provide reasonable time for us to address the issue before disclosing it publicly.

## Disclosure Policy

When we receive a security report, we will:

1. Confirm the issue and determine its severity
2. Develop and test a fix
3. Release a patched version as soon as possible
4. Publicly disclose the issue after a fix is available, crediting the reporter if desired

## Security Best Practices for Deployment

When running this service in production:

- **Enable HTTP Basic Authentication** using `-auth-username` and `-auth-password` (or `AUTH_USERNAME` / `AUTH_PASSWORD`) to prevent unauthorized cache access
- **Use TLS/HTTPS** by placing the service behind a reverse proxy or load balancer with TLS termination
- **Restrict network access** to the cache endpoint (`/cache/`) so it is reachable only by your build infrastructure
- **Rotate credentials regularly** and avoid hardcoding secrets in configuration files
- **Run with minimal privileges** — the service does not require root access
- **Monitor metrics** (`/metrics`) for unusual access patterns

## Security-Related Configuration

| Flag / Env Var | Purpose | Security Note |
| -------------- | ------- | ------------- |
| `-auth-username` / `AUTH_USERNAME` | HTTP Basic Auth user | Required in production |
| `-auth-password` / `AUTH_PASSWORD` | HTTP Basic Auth password | Use a strong, unique password |
| `-s3-endpoint` / `S3_ENDPOINT` | Custom S3 endpoint | Use TLS endpoints only |
| `-max-upload` / `MAX_UPLOAD_SIZE` | Max cache entry size | Set a reasonable limit to prevent abuse |

## Dependency Updates

We monitor dependencies for known vulnerabilities. If you are building from source, we recommend running `go mod tidy` and rebuilding regularly to pick up patched dependencies.

## Acknowledgments

We thank security researchers and community members who help keep this project secure.
