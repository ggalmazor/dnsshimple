package telemetry

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecordedInstrumentsAppearOnTheMetricsEndpoint(test *testing.T) {
	// Given the process metrics with some activity recorded
	metrics := NewMetrics()
	metrics.ActiveSessions.Inc()
	metrics.DNSimpleCalls.WithLabelValues("/zones", "200").Inc()
	metrics.RateLimited.Inc()
	metrics.BTXOutcomes.WithLabelValues("LinkAccount", "completed").Inc()

	// When Prometheus scrapes the handler
	scrape := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(scrape, httptest.NewRequest("GET", "/metrics", nil))

	// Then every instrument the design commits to is exposed with its recording
	exposition := scrape.Body.String()
	for _, expected := range []string{
		`dnsshimple_active_sessions 1`,
		`dnsshimple_dnsimple_calls_total{endpoint="/zones",status="200"} 1`,
		`dnsshimple_dnsimple_rate_limited_total 1`,
		`dnsshimple_btx_total{btx="LinkAccount",outcome="completed"} 1`,
	} {
		if !strings.Contains(exposition, expected) {
			test.Fatalf("expected %q in the scrape; got:\n%s", expected, exposition)
		}
	}
}
