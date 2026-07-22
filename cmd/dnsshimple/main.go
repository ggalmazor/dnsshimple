// dnsshimple is the production service entrypoint. As of M0 it boots
// telemetry and the ops endpoint; the SSH surface, the link bridge, and the
// Components arrive with M1 (DESIGN.md §11).
package main

import (
	"log/slog"
	"net/http"
	"os"

	"dnsshimple/core/telemetry"
)

func main() {
	logger := telemetry.NewLogger(os.Stdout, logLevelFromEnvironment())
	slog.SetDefault(logger)
	metrics := telemetry.NewMetrics()

	opsAddress := envOr("DNSSHIMPLE_OPS_ADDRESS", "localhost:9090")
	opsRouter := http.NewServeMux()
	opsRouter.Handle("GET /metrics", metrics.Handler())
	opsRouter.HandleFunc("GET /health", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok"))
	})

	slog.Info("ops endpoint listening", "address", opsAddress)
	if err := http.ListenAndServe(opsAddress, opsRouter); err != nil {
		slog.Error("ops endpoint died", "cause", err)
		os.Exit(1)
	}
}

func logLevelFromEnvironment() slog.Level {
	if os.Getenv("DNSSHIMPLE_DEBUG") != "" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
