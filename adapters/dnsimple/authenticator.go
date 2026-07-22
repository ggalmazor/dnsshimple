// Package dnsimple is the driven adapter that talks to the DNSimple API.
// As of M1 it covers the identity Authenticator port; the rate-limit-aware
// gateway grows here in M2 (DESIGN.md §5).
package dnsimple

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
	"dnsshimple/kit"
)

// Config carries the OAuth application and the DNSimple endpoints.
type Config struct {
	ClientID     string
	ClientSecret string
	AuthorizeURL string // browser-facing, e.g. https://sandbox.dnsimple.com/oauth/authorize
	APIBaseURL   string // e.g. https://api.sandbox.dnsimple.com/v2
	CallbackURL  string // the bridge's /callback
}

// Authenticator implements identity.Authenticator against the real API.
type Authenticator struct {
	config  Config
	web     *http.Client
	metrics *telemetry.Metrics
}

func NewAuthenticator(config Config, web *http.Client, metrics *telemetry.Metrics) Authenticator {
	return Authenticator{config: config, web: web, metrics: metrics}
}

var _ identity.Authenticator = Authenticator{}

func (authenticator Authenticator) AuthorizationPageURL(linkCode string) string {
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", authenticator.config.ClientID)
	query.Set("redirect_uri", authenticator.config.CallbackURL)
	query.Set("state", linkCode)
	return authenticator.config.AuthorizeURL + "?" + query.Encode()
}

// Redeem turns a browser grant into an asserted remote identity: token
// exchange, then whoami, as one semantic step on the railway.
func (authenticator Authenticator) Redeem(ctx context.Context, grantCode, linkCode string) kit.Outcome[identity.RemoteIdentity] {
	return kit.Chain(
		authenticator.exchangeGrant(ctx, grantCode, linkCode),
		func(accessToken string) kit.Outcome[identity.RemoteIdentity] {
			return authenticator.fetchIdentity(ctx, accessToken)
		})
}

func (authenticator Authenticator) exchangeGrant(ctx context.Context, grantCode, linkCode string) kit.Outcome[string] {
	requestBody, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     authenticator.config.ClientID,
		"client_secret": authenticator.config.ClientSecret,
		"code":          grantCode,
		"redirect_uri":  authenticator.config.CallbackURL,
		"state":         linkCode,
	})
	if err != nil {
		return kit.Failure[string](fmt.Errorf("encoding the token request: %w", err))
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		authenticator.config.APIBaseURL+"/oauth/access_token", bytes.NewReader(requestBody))
	if err != nil {
		return kit.Failure[string](fmt.Errorf("building the token request: %w", err))
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := authenticator.web.Do(request)
	if err != nil {
		return kit.Failure[string](fmt.Errorf("calling the token endpoint: %w", err))
	}
	defer response.Body.Close()
	authenticator.countCall("/oauth/access_token", response.StatusCode)
	if response.StatusCode != http.StatusOK {
		return kit.Failure[string](fmt.Errorf("token exchange rejected: DNSimple answered %s", response.Status))
	}

	var tokenPayload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&tokenPayload); err != nil {
		return kit.Failure[string](fmt.Errorf("decoding the token response: %w", err))
	}
	if tokenPayload.AccessToken == "" {
		return kit.Failure[string](fmt.Errorf("the token response carried no access_token"))
	}
	return kit.Success(tokenPayload.AccessToken)
}

func (authenticator Authenticator) fetchIdentity(ctx context.Context, accessToken string) kit.Outcome[identity.RemoteIdentity] {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		authenticator.config.APIBaseURL+"/whoami", nil)
	if err != nil {
		return kit.Failure[identity.RemoteIdentity](fmt.Errorf("building the whoami request: %w", err))
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)

	response, err := authenticator.web.Do(request)
	if err != nil {
		return kit.Failure[identity.RemoteIdentity](fmt.Errorf("calling whoami: %w", err))
	}
	defer response.Body.Close()
	authenticator.countCall("/whoami", response.StatusCode)
	if response.StatusCode != http.StatusOK {
		return kit.Failure[identity.RemoteIdentity](fmt.Errorf("whoami rejected: DNSimple answered %s", response.Status))
	}

	var whoamiPayload struct {
		Data struct {
			Account *struct {
				ID    int64  `json:"id"`
				Email string `json:"email"`
			} `json:"account"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&whoamiPayload); err != nil {
		return kit.Failure[identity.RemoteIdentity](fmt.Errorf("decoding the whoami response: %w", err))
	}
	if whoamiPayload.Data.Account == nil {
		return kit.Failure[identity.RemoteIdentity](fmt.Errorf("the token belongs to a user without an account context"))
	}
	return kit.Success(identity.RemoteIdentity{
		AccountID:   whoamiPayload.Data.Account.ID,
		Email:       whoamiPayload.Data.Account.Email,
		AccessToken: accessToken,
	})
}

func (authenticator Authenticator) countCall(endpoint string, status int) {
	authenticator.metrics.DNSimpleCalls.WithLabelValues(endpoint, strconv.Itoa(status)).Inc()
}
