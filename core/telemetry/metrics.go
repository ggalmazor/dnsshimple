package telemetry

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics owns the process-wide Prometheus registry and the instruments the
// design commits to (DESIGN.md §7). Components record through this façade;
// the ops endpoint exposes it for scraping.
type Metrics struct {
	registry *prometheus.Registry

	ActiveSessions     prometheus.Gauge
	SessionSeconds     prometheus.Histogram
	DNSimpleCalls      *prometheus.CounterVec
	RateLimited        prometheus.Counter
	RateLimitRemaining prometheus.Gauge
	CacheOutcomes      *prometheus.CounterVec
	BTXOutcomes        *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	metrics := &Metrics{
		registry: prometheus.NewRegistry(),
		ActiveSessions: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dnsshimple_active_sessions",
			Help: "SSH sessions currently connected.",
		}),
		SessionSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "dnsshimple_session_duration_seconds",
			Help:    "Duration of completed SSH sessions.",
			Buckets: prometheus.ExponentialBuckets(1, 4, 8), // 1s .. ~4.5h
		}),
		DNSimpleCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dnsshimple_dnsimple_calls_total",
			Help: "Calls to the DNSimple API by endpoint and response status.",
		}, []string{"endpoint", "status"}),
		RateLimited: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dnsshimple_dnsimple_rate_limited_total",
			Help: "Requests DNSimple rejected with 429.",
		}),
		RateLimitRemaining: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "dnsshimple_dnsimple_rate_limit_remaining",
			Help: "Remaining request budget reported by the last DNSimple response.",
		}),
		CacheOutcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dnsshimple_dnsimple_cache_total",
			Help: "Read-cache lookups by result.",
		}, []string{"result"}),
		BTXOutcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dnsshimple_btx_total",
			Help: "Business Transactions by type and outcome.",
		}, []string{"btx", "outcome"}),
	}
	metrics.registry.MustRegister(
		collectors.NewGoCollector(), // runtime health: goroutines, GC, memory
		metrics.ActiveSessions,
		metrics.SessionSeconds,
		metrics.DNSimpleCalls,
		metrics.RateLimited,
		metrics.RateLimitRemaining,
		metrics.CacheOutcomes,
		metrics.BTXOutcomes,
	)
	return metrics
}

func (metrics *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(metrics.registry, promhttp.HandlerOpts{})
}
