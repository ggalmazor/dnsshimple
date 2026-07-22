// dnsshimple is the production service: the SSH TUI, the link bridge, and
// the ops endpoint, wired around the identity Component (M1, DESIGN.md §11).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/ssh"

	"dnsshimple/adapters/bridge"
	"dnsshimple/adapters/dnsimple"
	"dnsshimple/adapters/postgres"
	"dnsshimple/adapters/sshui"
	"dnsshimple/adapters/vault"
	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
)

func main() {
	slog.SetDefault(telemetry.NewLogger(os.Stdout, logLevelFromEnvironment()))
	metrics := telemetry.NewMetrics()

	bootCtx, cancelBoot := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelBoot()

	// Storage: connect, migrate, done - every instance migrates on boot.
	pool, err := postgres.Connect(bootCtx,
		envOr("DATABASE_URL", "postgres://dnsshimple:dnsshimple@localhost:5433/dnsshimple"))
	exitOn("connecting to the database", err)
	defer pool.Close()
	exitOn("migrating the database", postgres.Migrate(bootCtx, pool))

	// The vault key is not optional: without it every stored token is lost
	// noise, so refuse to boot rather than degrade.
	tokenVault, err := vault.FromBase64Key(os.Getenv("DNSSHIMPLE_VAULT_KEY"))
	exitOn("loading DNSSHIMPLE_VAULT_KEY", err)

	bridgeAddress := envOr("DNSSHIMPLE_BRIDGE_ADDRESS", "localhost:8181")
	publicBridgeURL := envOr("DNSSHIMPLE_PUBLIC_BRIDGE_URL", "http://"+bridgeAddress)

	authenticator := dnsimple.NewAuthenticator(dnsimple.Config{
		ClientID:     mustEnv("DNSIMPLE_CLIENT_ID"),
		ClientSecret: mustEnv("DNSIMPLE_CLIENT_SECRET"),
		AuthorizeURL: envOr("DNSIMPLE_AUTHORIZE_URL", "https://sandbox.dnsimple.com/oauth/authorize"),
		APIBaseURL:   envOr("DNSIMPLE_API_URL", "https://api.sandbox.dnsimple.com/v2"),
		CallbackURL:  publicBridgeURL + "/callback",
	}, &http.Client{Timeout: 15 * time.Second}, metrics)

	identityComponent := identity.NewComponent(
		postgres.NewIdentityRepository(pool), tokenVault, authenticator, metrics)

	// The three listeners: bridge (browser leg), ops (Prometheus + health),
	// and SSH (the product).
	go serveHTTP("link bridge", bridgeAddress, bridge.LinkBridge{Identity: identityComponent}.Router())
	go serveHTTP("ops endpoint", envOr("DNSSHIMPLE_OPS_ADDRESS", "localhost:9090"), opsRouter(metrics))

	sshServer, err := sshui.NewServer(sshui.Config{
		Address:     envOr("DNSSHIMPLE_SSH_ADDRESS", "localhost:2222"),
		HostKeyPath: envOr("DNSSHIMPLE_HOST_KEY_PATH", ".spike/ssh_host_key"),
		LinkPageURL: publicBridgeURL + "/link",
	}, identityComponent, metrics)
	exitOn("building the SSH server", err)

	shutdownRequested := make(chan os.Signal, 1)
	signal.Notify(shutdownRequested, os.Interrupt, syscall.SIGTERM)
	go func() {
		slog.Info("SSH listening", "address", envOr("DNSSHIMPLE_SSH_ADDRESS", "localhost:2222"))
		if err := sshServer.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			slog.Error("SSH server died", "cause", err)
			os.Exit(1)
		}
	}()

	<-shutdownRequested
	slog.Info("shutting down")
	shutdownWindow, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = sshServer.Shutdown(shutdownWindow)
}

func opsRouter(metrics *telemetry.Metrics) *http.ServeMux {
	router := http.NewServeMux()
	router.Handle("GET /metrics", metrics.Handler())
	router.HandleFunc("GET /health", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok"))
	})
	return router
}

func serveHTTP(name, address string, router *http.ServeMux) {
	slog.Info(name+" listening", "address", address)
	if err := http.ListenAndServe(address, router); err != nil {
		slog.Error(name+" died", "cause", err)
		os.Exit(1)
	}
}

func exitOn(what string, err error) {
	if err != nil {
		slog.Error(what+" failed", "cause", err)
		os.Exit(1)
	}
}

func mustEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		slog.Error("required environment variable is missing", "name", name)
		os.Exit(1)
	}
	return value
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func logLevelFromEnvironment() slog.Level {
	if os.Getenv("DNSSHIMPLE_DEBUG") != "" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}
