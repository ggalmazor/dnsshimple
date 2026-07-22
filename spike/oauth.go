package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// OAuthGateway drives DNSimple's authorization-code flow. In fake mode the
// URLs point at the local stub instead of sandbox.dnsimple.com.
type OAuthGateway struct {
	ClientID     string
	ClientSecret string
	AuthorizeURL string // browser-facing, e.g. https://sandbox.dnsimple.com/oauth/authorize
	APIBaseURL   string // e.g. https://api.sandbox.dnsimple.com/v2
	CallbackURL  string
	Web          *http.Client
}

// BrowserAuthorizationURL is where the bridge sends the user's browser,
// with the link code riding along as the OAuth state parameter.
func (gateway OAuthGateway) BrowserAuthorizationURL(linkCode string) string {
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", gateway.ClientID)
	query.Set("redirect_uri", gateway.CallbackURL)
	query.Set("state", linkCode)
	return gateway.AuthorizeURL + "?" + query.Encode()
}

// RedeemAuthorizationCode exchanges the grant code for an access token
// (POST /oauth/access_token, per the DNSimple OpenAPI spec).
func (gateway OAuthGateway) RedeemAuthorizationCode(grantCode, linkCode string) Outcome[string] {
	requestBody, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     gateway.ClientID,
		"client_secret": gateway.ClientSecret,
		"code":          grantCode,
		"redirect_uri":  gateway.CallbackURL,
		"state":         linkCode,
	})
	if err != nil {
		return Failure[string](fmt.Errorf("encoding token request: %w", err))
	}

	response, err := gateway.Web.Post(
		gateway.APIBaseURL+"/oauth/access_token", "application/json", bytes.NewReader(requestBody))
	if err != nil {
		return Failure[string](fmt.Errorf("calling token endpoint: %w", err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Failure[string](fmt.Errorf("token exchange rejected: DNSimple answered %s", response.Status))
	}

	var tokenPayload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&tokenPayload); err != nil {
		return Failure[string](fmt.Errorf("decoding token response: %w", err))
	}
	if tokenPayload.AccessToken == "" {
		return Failure[string](fmt.Errorf("token response carried no access_token"))
	}
	return Success(tokenPayload.AccessToken)
}

// FetchIdentity resolves the token owner via GET /whoami.
func (gateway OAuthGateway) FetchIdentity(accessToken string) Outcome[LinkedAccount] {
	request, err := http.NewRequest(http.MethodGet, gateway.APIBaseURL+"/whoami", nil)
	if err != nil {
		return Failure[LinkedAccount](fmt.Errorf("building whoami request: %w", err))
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)

	response, err := gateway.Web.Do(request)
	if err != nil {
		return Failure[LinkedAccount](fmt.Errorf("calling whoami: %w", err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Failure[LinkedAccount](fmt.Errorf("whoami rejected: DNSimple answered %s", response.Status))
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
		return Failure[LinkedAccount](fmt.Errorf("decoding whoami response: %w", err))
	}
	if whoamiPayload.Data.Account == nil {
		return Failure[LinkedAccount](fmt.Errorf("token belongs to a user without an account context"))
	}
	return Success(LinkedAccount{
		AccountID:   whoamiPayload.Data.Account.ID,
		Email:       whoamiPayload.Data.Account.Email,
		AccessToken: accessToken,
	})
}
