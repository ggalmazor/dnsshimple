package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"dnsshimple/adapters/admintui"
	"dnsshimple/adapters/dnsimple"
	"dnsshimple/adapters/jobs"
	"dnsshimple/adapters/postgres"
	"dnsshimple/adapters/vault"
	"dnsshimple/core/admin"
	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
)

// runAdmin is the operator console: `docker exec -it <container>
// /dnsshimple admin` (or deploy/admin.sh). It shares the server's env, so
// it reaches the same database and vault, but it starts no listeners and
// its River client is insert-only - nothing here can work jobs. Logs go
// to stderr; stdout belongs to the TUI.
func runAdmin() {
	slog.SetDefault(telemetry.NewLogger(os.Stderr, logLevelFromEnvironment()))
	metrics := telemetry.NewMetrics()

	bootCtx, cancelBoot := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelBoot()

	// No migrations here: the server migrates on boot, and an admin session
	// against a not-yet-migrated database has nothing to administer anyway.
	pool, err := postgres.Connect(bootCtx,
		envOr("DATABASE_URL", "postgres://dnsshimple:dnsshimple@localhost:5433/dnsshimple"))
	exitOn("connecting to the database", err)
	defer pool.Close()

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

	riverClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	exitOn("building the River client", err)

	adminComponent := admin.NewComponent(
		postgres.NewStatistics(pool), jobs.NewAdmin(riverClient, pool),
		identityComponent, postgres.NewMaintenance(pool))

	exitOn("running the admin console", admintui.Run(context.Background(), admintui.Components{
		Admin:    adminComponent,
		Identity: identityComponent,
	}))
}
