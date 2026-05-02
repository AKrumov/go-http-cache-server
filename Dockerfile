# Build stage
FROM golang:1-alpine AS builder

WORKDIR /app

# Download dependencies first (better layer caching)
COPY app/go.mod app/go.sum ./
RUN go mod download

# Copy source and build static binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o gradle-cache-server ./app

# Final stage
FROM alpine:3.20

# Install CA certificates for S3 TLS and create non-root user
RUN apk add --no-cache ca-certificates && \
    adduser -D -H -s /bin/false cacheuser

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/gradle-cache-server /app/gradle-cache-server

USER cacheuser

EXPOSE 8080

ENTRYPOINT ["/app/gradle-cache-server"]
