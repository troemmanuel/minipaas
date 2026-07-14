package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests processed.",
	}, []string{"method", "path", "status"})

	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Latency of HTTP requests.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	EventsPublishedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "items_events_published_total",
		Help: "Total number of item events published to RabbitMQ.",
	})

	EventsConsumedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "items_events_consumed_total",
		Help: "Total number of item events consumed from RabbitMQ.",
	}, []string{"type", "outcome"})
)

// Instrument wraps an http.HandlerFunc with request count/latency metrics.
// routePattern is the templated path (e.g. "/items/{id}") used as a low-cardinality label.
func Instrument(routePattern string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next(rec, r)
		HTTPRequestsTotal.WithLabelValues(r.Method, routePattern, strconv.Itoa(rec.status)).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, routePattern).Observe(time.Since(start).Seconds())
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
