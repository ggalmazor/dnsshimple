// Package sshui is the driving adapter that serves the TUI over SSH: Wish
// terminates the connection, the fingerprint identifies the tenant, and
// Bubble Tea renders whatever the identity Component answers.
package sshui

import (
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	"github.com/charmbracelet/wish/bubbletea"
	gossh "golang.org/x/crypto/ssh"

	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
)

type Config struct {
	Address     string
	HostKeyPath string
	LinkPageURL string // public URL of the bridge's /link page, shown in the TUI
}

func NewServer(config Config, identityComponent *identity.Component, metrics *telemetry.Metrics) (*ssh.Server, error) {
	return wish.NewServer(
		wish.WithAddress(config.Address),
		wish.WithHostKeyPath(config.HostKeyPath),
		// Every key is accepted: the fingerprint itself is the identity, and
		// unlinked keys land in the link flow instead of being rejected.
		wish.WithPublicKeyAuth(func(_ ssh.Context, _ ssh.PublicKey) bool { return true }),
		wish.WithMiddleware(
			bubbletea.Middleware(sessionHandler(identityComponent, config.LinkPageURL)),
			activeterm.Middleware(),
			sessionMetrics(metrics),
		),
	)
}

// sessionMetrics feeds the session gauge and duration histogram; it replaces
// wish's stock logging middleware, which logs the whole client command line
// (terminals like Warp send kilobytes of shell bootstrap there).
func sessionMetrics(metrics *telemetry.Metrics) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(session ssh.Session) {
			connectedAt := time.Now()
			metrics.ActiveSessions.Inc()
			slog.Info("session connected", "user", session.User(), "remote", session.RemoteAddr().String())
			defer func() {
				metrics.ActiveSessions.Dec()
				metrics.SessionSeconds.Observe(time.Since(connectedAt).Seconds())
				slog.Info("session closed", "duration", time.Since(connectedAt).Round(time.Millisecond).String())
			}()
			next(session)
		}
	}
}

// sessionHandler decides the first screen: linked tenants get their session,
// unknown keys get the link flow.
func sessionHandler(identityComponent *identity.Component, linkPageURL string) bubbletea.Handler {
	return func(session ssh.Session) (tea.Model, []tea.ProgramOption) {
		options := []tea.ProgramOption{tea.WithAltScreen()}

		publicKey := session.PublicKey()
		if publicKey == nil {
			return newFailedModel("Connect with an SSH key; password logins cannot be linked."), options
		}
		fingerprint := gossh.FingerprintSHA256(publicKey)
		ctx := telemetry.WithSession(session.Context(), fingerprint)

		if ready := identityComponent.SessionFor(ctx, fingerprint); !ready.Failed() {
			slog.InfoContext(ctx, "returning tenant", "account_id", ready.Value().Tenant.AccountID)
			return newLinkedModel(fingerprint, ready.Value()), options
		}

		pending := identityComponent.BeginLink(ctx, fingerprint)
		if pending.Failed() {
			slog.ErrorContext(ctx, "BeginLink failed", "cause", pending.Error())
			return newFailedModel("Could not start the link flow; try again in a minute."), options
		}
		slog.InfoContext(ctx, "issued link code")
		return newWaitingModel(waitingScreen{
			identity:    identityComponent,
			ctx:         ctx,
			fingerprint: fingerprint,
			linkPageURL: linkPageURL,
			pending:     pending.Value(),
		}), options
	}
}
