package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Grey-box behavior test of the whole browser handshake, SSH leg excluded:
// it drives the bridge exactly like a browser would and asserts only on
// observable outcomes (final page, the woken session channel, the store).
func TestBrowserHandshakeLinksAnSSHFingerprintToADNSimpleAccount(test *testing.T) {
	// Given a DNSimple (fake), a running bridge, and an SSH session waiting on a link code
	dnsimple := httptest.NewServer(NewFakeDNSimple())
	defer dnsimple.Close()

	// The bridge needs its own public URL (for the OAuth callback) before it
	// starts, so reserve the listener first.
	bridgeListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		test.Fatalf("reserving bridge listener: %v", err)
	}
	bridgeURL := "http://" + bridgeListener.Addr().String()

	identities := NewIdentityStore()
	bridge := LinkBridge{
		Identities: identities,
		OAuth: OAuthGateway{
			ClientID:     "spike-test-client",
			ClientSecret: "spike-test-secret",
			AuthorizeURL: dnsimple.URL + "/oauth/authorize",
			APIBaseURL:   dnsimple.URL + "/v2",
			CallbackURL:  bridgeURL + "/callback",
			Web:          dnsimple.Client(),
		},
	}
	bridgeServer := httptest.NewUnstartedServer(bridge.Router())
	bridgeServer.Listener.Close()
	bridgeServer.Listener = bridgeListener
	bridgeServer.Start()
	defer bridgeServer.Close()

	sessionFingerprint := "SHA256:test-fingerprint"
	pendingLink := identities.BeginLink(sessionFingerprint)

	// When the user submits the code in the browser (redirects followed
	// automatically: /link -> authorize -> callback)
	browser := &http.Client{Timeout: 5 * time.Second}
	finalPage, err := browser.PostForm(bridgeServer.URL+"/link",
		url.Values{"code": {strings.ToLower(DisplayLinkCode(pendingLink.Code))}}) // typed sloppily on purpose
	if err != nil {
		test.Fatalf("browser flow failed: %v", err)
	}
	defer finalPage.Body.Close()

	// Then the final page confirms the link
	pageBody, _ := io.ReadAll(finalPage.Body)
	if finalPage.StatusCode != http.StatusOK {
		test.Fatalf("expected 200 at the end of the handshake, got %s: %s", finalPage.Status, pageBody)
	}
	if !strings.Contains(string(pageBody), "Return to your terminal") {
		test.Fatalf("final page is not the success page: %s", pageBody)
	}

	// And the waiting SSH session is woken with the linked account
	select {
	case account := <-pendingLink.Completed:
		if account.AccountID != 1010 || account.Email != "demo@dnsshimple.test" {
			test.Fatalf("session woke with wrong account: %+v", account)
		}
		if account.AccessToken == "" {
			test.Fatal("session woke without an access token")
		}
	case <-time.After(2 * time.Second):
		test.Fatal("SSH session was never woken by the handshake")
	}

	// And a reconnect with the same key finds the account (no browser needed)
	if _, known := identities.AccountFor(sessionFingerprint); !known {
		test.Fatal("fingerprint was not bound to the account")
	}

	// And the code is single-use
	if _, stillAlive := identities.PendingLink(pendingLink.Code); stillAlive {
		test.Fatal("link code survived its own use")
	}
}
