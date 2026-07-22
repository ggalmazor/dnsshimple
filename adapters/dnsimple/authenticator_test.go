package dnsimple

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dnsshimple/core/telemetry"
)

func testAuthenticator(test *testing.T) (Authenticator, *telemetry.Metrics) {
	test.Helper()
	fake := httptest.NewServer(NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"}))
	test.Cleanup(fake.Close)
	metrics := telemetry.NewMetrics()
	authenticator := NewAuthenticator(Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		AuthorizeURL: fake.URL + "/oauth/authorize",
		APIBaseURL:   fake.URL,
		CallbackURL:  "http://localhost:8181/callback",
	}, fake.Client(), metrics)
	return authenticator, metrics
}

func TestRedeemTurnsAGrantIntoARemoteIdentityAndCountsTheCalls(test *testing.T) {
	// Given DNSimple (the fake) and the OAuth application
	authenticator, metrics := testAuthenticator(test)

	// When a browser grant is redeemed
	outcome := authenticator.Redeem(context.Background(), "fake-grant-code", "KZ3Q7HWP")

	// Then the asserted identity carries the account and the token
	if outcome.Failed() {
		test.Fatalf("redeem failed: %v", outcome.Error())
	}
	remote := outcome.Value()
	if remote.AccountID != 2416 || remote.Email != "guille@sendia.org" || remote.AccessToken == "" {
		test.Fatalf("wrong remote identity: %+v", remote)
	}

	// And both API calls landed on the usage metrics
	scrape := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(scrape, httptest.NewRequest("GET", "/metrics", nil))
	for _, expected := range []string{
		`dnsshimple_dnsimple_calls_total{endpoint="/oauth/access_token",status="200"} 1`,
		`dnsshimple_dnsimple_calls_total{endpoint="/whoami",status="200"} 1`,
	} {
		if !strings.Contains(scrape.Body.String(), expected) {
			test.Fatalf("missing %q in the scrape", expected)
		}
	}
}

func TestRedeemFailsWhenDNSimpleRejectsTheGrant(test *testing.T) {
	// Given a DNSimple that rejects every exchange
	rejecting := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, `{"message":"invalid grant"}`, http.StatusBadRequest)
	}))
	test.Cleanup(rejecting.Close)
	authenticator := NewAuthenticator(Config{
		APIBaseURL: rejecting.URL,
	}, rejecting.Client(), telemetry.NewMetrics())

	// When the grant is redeemed
	outcome := authenticator.Redeem(context.Background(), "bad-grant", "KZ3Q7HWP")

	// Then the railway fails with the rejection, not a panic or a zero value
	if !outcome.Failed() || !strings.Contains(outcome.Error().Error(), "400") {
		test.Fatalf("expected a 400 rejection, got %v", outcome.Error())
	}
}
