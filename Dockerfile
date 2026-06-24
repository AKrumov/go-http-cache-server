# Build stage
FROM golang:1-alpine AS builder

WORKDIR /src

# Download dependencies first (better layer caching)
COPY app/go.mod app/go.sum ./app/
WORKDIR /src/app
RUN go mod download

# Copy source and build static binary
COPY app/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /go-http-cache-server .

# Final stage
FROM alpine:3.20

# Install CA certificates for S3 TLS and create non-root user
RUN apk add --no-cache ca-certificates && \
    addgroup -g 1000 cacheuser && \
    adduser -D -H -s /bin/false -u 1000 -G cacheuser cacheuser

WORKDIR /app

RUN mkdir -p /app/cache-data && \
    chown -R 1000:1000 /app

# Copy binary from builder
COPY --from=builder /go-http-cache-server /app/go-http-cache-server

USER 1000:1000

EXPOSE 8080

ENTRYPOINT ["/app/go-http-cache-server"]
