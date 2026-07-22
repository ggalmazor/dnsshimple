// Spike: validate the SSH link-code OAuth approach end to end.
//
// Run:            cd spike && go run .          (fake DNSimple, zero config)
// Real sandbox:   DNSIMPLE_CLIENT_ID=... DNSIMPLE_CLIENT_SECRET=... go run .
// Then:           ssh -p 2222 localhost
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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	gossh "golang.org/x/crypto/ssh"
)

func main() {
	sshAddress := envOr("SPIKE_SSH_ADDRESS", "localhost:2222")
	bridgeAddress := envOr("SPIKE_BRIDGE_ADDRESS", "localhost:8181")
	fakeAddress := envOr("SPIKE_FAKE_DNSIMPLE_ADDRESS", "localhost:8282")

	identities := NewIdentityStore()
	oauth := configureOAuth(bridgeAddress, fakeAddress)
	bridge := LinkBridge{Identities: identities, OAuth: oauth}

	go func() {
		slog.Info("link bridge listening", "url", "http://"+bridgeAddress+"/link")
		if err := http.ListenAndServe(bridgeAddress, bridge.Router()); err != nil {
			slog.Error("link bridge died", "cause", err)
			os.Exit(1)
		}
	}()

	sshServer, err := wish.NewServer(
		wish.WithAddress(sshAddress),
		wish.WithHostKeyPath(".spike/ssh_host_key"),
		wish.WithPublicKeyAuth(func(_ ssh.Context, _ ssh.PublicKey) bool {
			return true // spike accepts every key; identity is the fingerprint itself
		}),
		wish.WithMiddleware(
			bubbletea.Middleware(makeSessionHandler(identities, bridgeAddress)),
			activeterm.Middleware(),
			logging.Middleware(),
		),
	)
	if err != nil {
		slog.Error("building SSH server failed", "cause", err)
		os.Exit(1)
	}

	shutdownRequested := make(chan os.Signal, 1)
	signal.Notify(shutdownRequested, os.Interrupt, syscall.SIGTERM)
	go func() {
		slog.Info("SSH listening", "hint", "ssh -p 2222 localhost", "address", sshAddress)
		if err := sshServer.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			slog.Error("SSH server died", "cause", err)
			os.Exit(1)
		}
	}()

	<-shutdownRequested
	slog.Info("shutting down")
	shutdownWindow, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = sshServer.Shutdown(shutdownWindow)
}

// makeSessionHandler decides, per SSH connection, between the "welcome back"
// screen (known fingerprint) and the link-code screen (unknown fingerprint).
func makeSessionHandler(identities *IdentityStore, bridgeAddress string) bubbletea.Handler {
	linkPageURL := "http://" + bridgeAddress + "/link"
	return func(session ssh.Session) (tea.Model, []tea.ProgramOption) {
		options := []tea.ProgramOption{tea.WithAltScreen()}

		publicKey := session.PublicKey()
		if publicKey == nil {
			// Password/none auth slipped through; treat as unlinkable guest.
			return newWaitingModel("(no public key)", linkPageURL,
				identities.BeginLink("no-key:"+session.RemoteAddr().String())), options
		}
		fingerprint := gossh.FingerprintSHA256(publicKey)

		if account, alreadyLinked := identities.AccountFor(fingerprint); alreadyLinked {
			slog.Info("returning user", "fingerprint", fingerprint, "account_id", account.AccountID)
			return newLinkedModel(fingerprint, account), options
		}

		pendingLink := identities.BeginLink(fingerprint)
		slog.Info("issued link code", "fingerprint", fingerprint, "code", pendingLink.Code)
		return newWaitingModel(fingerprint, linkPageURL, pendingLink), options
	}
}

// configureOAuth wires the gateway against the real DNSimple sandbox when
// client credentials exist, or spins up the local fake when they do not.
func configureOAuth(bridgeAddress, fakeAddress string) OAuthGateway {
	callbackURL := "http://" + bridgeAddress + "/callback"
	webClient := &http.Client{Timeout: 15 * time.Second}

	clientID := os.Getenv("DNSIMPLE_CLIENT_ID")
	if clientID != "" {
		return OAuthGateway{
			ClientID:     clientID,
			ClientSecret: os.Getenv("DNSIMPLE_CLIENT_SECRET"),
			AuthorizeURL: envOr("DNSIMPLE_AUTHORIZE_URL", "https://sandbox.dnsimple.com/oauth/authorize"),
			APIBaseURL:   envOr("DNSIMPLE_API_URL", "https://api.sandbox.dnsimple.com/v2"),
			CallbackURL:  callbackURL,
			Web:          webClient,
		}
	}

	slog.Info("no DNSIMPLE_CLIENT_ID: starting fake DNSimple", "address", fakeAddress)
	go func() {
		if err := http.ListenAndServe(fakeAddress, NewFakeDNSimple()); err != nil {
			slog.Error("fake DNSimple died", "cause", err)
			os.Exit(1)
		}
	}()
	return OAuthGateway{
		ClientID:     "fake-client",
		ClientSecret: "fake-secret",
		AuthorizeURL: "http://" + fakeAddress + "/oauth/authorize",
		APIBaseURL:   "http://" + fakeAddress + "/v2",
		CallbackURL:  callbackURL,
		Web:          webClient,
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
