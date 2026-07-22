package bridge

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"dnsshimple/adapters/dnsimple"
	"dnsshimple/adapters/memory"
	"dnsshimple/adapters/vault"
	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
)

// Grey-box over the whole browser leg with the real Component, the real
// secretbox vault, and the real Authenticator adapter against the DNSimple
// fake; only persistence is in-memory. Assertions are the observable
// outcomes: the final page, the woken session, and the opened token.
func TestTheBrowserLegLinksATenantAndWakesTheParkedSession(test *testing.T) {
	// Given an SSH session waiting on a fresh link code
	fakeDNSimple := httptest.NewServer(dnsimple.NewFake(dnsimple.FakeAccount{ID: 2416, Email: "guille@sendia.org"}))
	test.Cleanup(fakeDNSimple.Close)

	tokenVault, err := vault.FromBase64Key(vault.NewRandomBase64Key())
	if err != nil {
		test.Fatalf("building the vault: %v", err)
	}

	metrics := telemetry.NewMetrics()
	repository := memory.NewIdentityRepository()
	authenticator := dnsimple.NewAuthenticator(dnsimple.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		AuthorizeURL: fakeDNSimple.URL + "/oauth/authorize",
		APIBaseURL:   fakeDNSimple.URL,
		CallbackURL:  "PLACEHOLDER", // set below, once the bridge has its URL
	}, fakeDNSimple.Client(), metrics)

	component := identity.NewComponent(repository, tokenVault, authenticator, metrics)
	pending := component.BeginLink(context.Background(), "SHA256:laptop")
	if pending.Failed() {
		test.Fatalf("BeginLink: %v", pending.Error())
	}

	// The bridge needs its public URL inside the authenticator config, so
	// reserve the server first and rebuild the wiring with the real URL.
	bridgeServer := httptest.NewServer(http.NotFoundHandler())
	test.Cleanup(bridgeServer.Close)
	authenticator = dnsimple.NewAuthenticator(dnsimple.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		AuthorizeURL: fakeDNSimple.URL + "/oauth/authorize",
		APIBaseURL:   fakeDNSimple.URL,
		CallbackURL:  bridgeServer.URL + "/callback",
	}, fakeDNSimple.Client(), metrics)
	component = identity.NewComponent(repository, tokenVault, authenticator, metrics)
	bridgeServer.Config.Handler = LinkBridge{Identity: component}.Router()

	parked := make(chan identity.Session, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		outcome := component.AwaitLink(ctx, pending.Value().Code, "SHA256:laptop")
		if !outcome.Failed() {
			parked <- outcome.Value()
		}
	}()

	// When the user completes the browser leg, typing the code sloppily
	browser := &http.Client{Timeout: 5 * time.Second}
	finalPage, err := browser.PostForm(bridgeServer.URL+"/link",
		url.Values{"code": {strings.ToLower(identity.DisplayLinkCode(pending.Value().Code))}})
	if err != nil {
		test.Fatalf("browser flow failed: %v", err)
	}
	defer finalPage.Body.Close()

	// Then the final page confirms the link
	pageBody, _ := io.ReadAll(finalPage.Body)
	if finalPage.StatusCode != http.StatusOK || !strings.Contains(string(pageBody), "Return to your terminal") {
		test.Fatalf("not the success page (%s): %s", finalPage.Status, pageBody)
	}

	// And the parked session wakes with a working, opened token
	select {
	case session := <-parked:
		if session.Tenant.AccountID != 2416 || session.AccessToken != "fake-access-token-2416" {
			test.Fatalf("session woke wrong: %+v", session)
		}
	case <-time.After(5 * time.Second):
		test.Fatal("the parked session never woke")
	}
}

func TestAMadeUpCodeNeverReachesDNSimple(test *testing.T) {
	// Given a bridge whose DNSimple would fail the test if touched
	touched := false
	angryDNSimple := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		touched = true
	}))
	test.Cleanup(angryDNSimple.Close)

	metrics := telemetry.NewMetrics()
	tokenVault, _ := vault.FromBase64Key(vault.NewRandomBase64Key())
	component := identity.NewComponent(
		memory.NewIdentityRepository(),
		tokenVault,
		dnsimple.NewAuthenticator(dnsimple.Config{APIBaseURL: angryDNSimple.URL},
			angryDNSimple.Client(), metrics),
		metrics)
	bridgeServer := httptest.NewServer(LinkBridge{Identity: component}.Router())
	test.Cleanup(bridgeServer.Close)

	// When a made-up code is submitted
	browser := &http.Client{Timeout: 2 * time.Second}
	page, err := browser.PostForm(bridgeServer.URL+"/link", url.Values{"code": {"AAAA-2222"}})
	if err != nil {
		test.Fatalf("submitting: %v", err)
	}
	defer page.Body.Close()

	// Then the user sees the unknown-code page and DNSimple was never called
	if page.StatusCode != http.StatusNotFound {
		test.Fatalf("expected 404, got %s", page.Status)
	}
	if touched {
		test.Fatal("DNSimple was called for a code that never existed")
	}
}
