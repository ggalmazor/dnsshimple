package dnsimple

import (
	"fmt"
	"net/http"
	"net/url"
)

// FakeAccount is what NewFake asserts about every redeemed token.
type FakeAccount struct {
	ID    int64
	Email string
}

// NewFake stands in for DNSimple in grey-box tests: the three endpoints the
// handshake touches, honoring only the grant code "fake-grant-code". It will
// grow spec-checked behavior (rate limits, error payloads) with the M2
// gateway work (DESIGN.md §8).
func NewFake(account FakeAccount) *http.ServeMux {
	fake := http.NewServeMux()

	// The consent screen of a real DNSimple, collapsed to an instant yes.
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

	fake.HandleFunc("POST /oauth/access_token", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(response, `{"access_token":"fake-access-token-%d","token_type":"Bearer"}`, account.ID)
	})

	fake.HandleFunc("GET /whoami", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(response,
			`{"data":{"user":null,"account":{"id":%d,"email":%q}}}`, account.ID, account.Email)
	})

	return fake
}
