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
		}, []string{"method", "handler", "status", "cache_id"},
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
)

func init() {
	prometheus.MustRegister(
		requestCounter,
		requestDuration,
		cacheHitCounter,
		cacheMissCounter,
		cacheEntriesStored,
		cacheStoredBytes,
		cacheServedBytes,
		inFlightRequests,
	)
}

func Handler() http.Handler {
	return promhttp.Handler()
}

func ObserveRequest(method, handlerName, status, cacheID string, duration time.Duration) {
	requestCounter.WithLabelValues(method, handlerName, status, cacheID).Inc()
	requestDuration.WithLabelValues(method, handlerName, status, cacheID).Observe(duration.Seconds())
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
