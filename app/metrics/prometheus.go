package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	requestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gradle_cache_requests_total",
			Help: "Total HTTP requests received by the cache server.",
		}, []string{"method", "handler", "status", "cache_id"},
	)
	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gradle_cache_request_duration_seconds",
			Help:    "Duration of HTTP requests handled by the cache server.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "handler", "status"},
	)
	cacheHitCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gradle_cache_hits_total",
			Help: "Total number of cache hits.",
		}, []string{"cache_id"},
	)
	cacheMissCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gradle_cache_misses_total",
			Help: "Total number of cache misses.",
		}, []string{"cache_id"},
	)
	cacheEntriesStored = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gradle_cache_entries_stored_total",
			Help: "Total number of cache entries successfully stored.",
		}, []string{"cache_id"},
	)
	cacheEntriesDeleted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gradle_cache_entries_deleted_total",
			Help: "Total number of cache entries successfully deleted.",
		}, []string{"cache_id"},
	)
	cacheStoredBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gradle_cache_stored_bytes_total",
			Help: "Total number of bytes stored in the cache.",
		}, []string{"cache_id"},
	)
	cacheServedBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gradle_cache_served_bytes_total",
			Help: "Total number of bytes served from the cache.",
		}, []string{"cache_id"},
	)
	inFlightRequests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gradle_cache_in_flight_requests",
			Help: "Number of in-flight HTTP requests currently being handled.",
		},
	)
	// Additional observability metrics
	s3RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gradle_cache_s3_request_duration_seconds",
			Help:    "Duration of S3 requests by operation and result.",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation", "result"},
	)
	localCacheEntries = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gradle_cache_local_entries_total",
			Help: "Number of entries in the local cache.",
		},
	)
	localCacheSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gradle_cache_local_size_bytes",
			Help: "Total size of the local cache in bytes.",
		},
	)
	localCleanupRuns = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gradle_cache_local_cleanup_runs_total",
			Help: "Total number of local cache cleanup runs.",
		},
	)
	localCleanupEvicted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gradle_cache_local_cleanup_evicted_bytes_total",
			Help: "Total bytes evicted by local cache cleanup.",
		},
	)
	circuitBreakerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gradle_cache_circuit_breaker_state",
			Help: "Circuit breaker state: 0=closed, 1=open, 2=half-open.",
		}, []string{"backend"},
	)
	rateLimitHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gradle_cache_rate_limit_hits_total",
			Help: "Total number of requests rejected due to rate limiting.",
		},
	)
	memoryCacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gradle_cache_memory_hits_total",
			Help: "Total number of in-memory cache hits.",
		},
	)
	memoryCacheMisses = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gradle_cache_memory_misses_total",
			Help: "Total number of in-memory cache misses.",
		},
	)
)

func init() {
	prometheus.MustRegister(
		requestCounter,
		requestDuration,
		cacheHitCounter,
		cacheMissCounter,
		cacheEntriesStored,
		cacheEntriesDeleted,
		cacheStoredBytes,
		cacheServedBytes,
		inFlightRequests,
		s3RequestDuration,
		localCacheEntries,
		localCacheSize,
		localCleanupRuns,
		localCleanupEvicted,
		circuitBreakerState,
		rateLimitHits,
		memoryCacheHits,
		memoryCacheMisses,
	)
}

func Handler() http.Handler {
	return promhttp.Handler()
}

func ObserveRequest(method, handlerName, status, cacheID string, duration time.Duration) {
	requestCounter.WithLabelValues(method, handlerName, status, cacheID).Inc()
	requestDuration.WithLabelValues(method, handlerName, status).Observe(duration.Seconds())
}

func CacheHit(cacheID string) {
	cacheHitCounter.WithLabelValues(cacheID).Inc()
}

func CacheMiss(cacheID string) {
	cacheMissCounter.WithLabelValues(cacheID).Inc()
}

func CacheEntryStored(cacheID string) {
	cacheEntriesStored.WithLabelValues(cacheID).Inc()
}

func CacheEntryDeleted(cacheID string) {
	cacheEntriesDeleted.WithLabelValues(cacheID).Inc()
}

func CacheStoredBytes(cacheID string, bytes int64) {
	cacheStoredBytes.WithLabelValues(cacheID).Add(float64(bytes))
}

func CacheServedBytes(cacheID string, bytes int64) {
	cacheServedBytes.WithLabelValues(cacheID).Add(float64(bytes))
}

func InFlightInc() {
	inFlightRequests.Inc()
}

func InFlightDec() {
	inFlightRequests.Dec()
}

// S3RequestDuration observes the duration of an S3 operation.
func S3RequestDuration(operation, result string, duration time.Duration) {
	s3RequestDuration.WithLabelValues(operation, result).Observe(duration.Seconds())
}

// SetLocalCacheEntries updates the local cache entries gauge.
func SetLocalCacheEntries(n float64) {
	localCacheEntries.Set(n)
}

// SetLocalCacheSize updates the local cache size gauge.
func SetLocalCacheSize(bytes float64) {
	localCacheSize.Set(bytes)
}

// LocalCleanupRun increments the cleanup run counter.
func LocalCleanupRun() {
	localCleanupRuns.Inc()
}

// LocalCleanupEvicted adds evicted bytes to the counter.
func LocalCleanupEvicted(bytes float64) {
	localCleanupEvicted.Add(bytes)
}

// SetCircuitBreakerState sets the circuit breaker state gauge.
func SetCircuitBreakerState(backend string, state float64) {
	circuitBreakerState.WithLabelValues(backend).Set(state)
}

// RateLimitHit increments the rate limit hit counter.
func RateLimitHit() {
	rateLimitHits.Inc()
}

// MemoryCacheHit increments the memory cache hit counter.
func MemoryCacheHit() {
	memoryCacheHits.Inc()
}

// MemoryCacheMiss increments the memory cache miss counter.
func MemoryCacheMiss() {
	memoryCacheMisses.Inc()
}
