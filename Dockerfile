# Build stage
FROM golang:1-alpine AS builder

WORKDIR /src

# Download dependencies first (better layer caching)
COPY app/go.mod app/go.sum ./app/
WORKDIR /src/app
RUN go mod download

# Copy source and build static binary
COPY app/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /go-gradle-cache .

# Final stage
FROM alpine:3.20

# Install CA certificates for S3 TLS and create non-root user
RUN apk add --no-cache ca-certificates && \
    adduser -D -H -s /bin/false cacheuser

WORKDIR /app

# Copy binary from builder
COPY --from=builder /go-gradle-cache /app/go-gradle-cache

USER cacheuser

EXPOSE 8080

ENTRYPOINT ["/app/go-gradle-cache"]
