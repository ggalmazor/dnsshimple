package main

import (
	"fmt"
	"net/http"
	"net/url"
)

// NewFakeDNSimple stands in for sandbox.dnsimple.com until we have a
// registered OAuth application. It implements the three endpoints the
// handshake touches: authorize (browser), token exchange, and whoami.
func NewFakeDNSimple() *http.ServeMux {
	fake := http.NewServeMux()

	// Browser leg: a real DNSimple would show a consent screen here.
	// The fake consents instantly and bounces back with a grant code.
	fake.HandleFunc("GET /oauth/authorize", func(response http.ResponseWriter, request *http.Request) {
		redirectTarget, err := url.Parse(request.URL.Query().Get("redirect_uri"))
		if err != nil || redirectTarget.String() == "" {
			http.Error(response, "missing redirect_uri", http.StatusBadRequest)
			return
		}
		bounceQuery := redirectTarget.Query()
		bounceQuery.Set("code", "fake-grant-code")
		bounceQuery.Set("state", request.URL.Query().Get("state"))
		redirectTarget.RawQuery = bounceQuery.Encode()
		http.Redirect(response, request, redirectTarget.String(), http.StatusFound)
	})

	fake.HandleFunc("POST /v2/oauth/access_token", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprint(response, `{"access_token":"fake-access-token-1010","token_type":"Bearer"}`)
	})

	fake.HandleFunc("GET /v2/whoami", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprint(response, `{"data":{"user":null,"account":{"id":1010,"email":"demo@dnsshimple.test"}}}`)
	})

	return fake
}
