// Package bridge is the driving adapter that carries the browser leg of the
// link handshake (DESIGN.md §4): the user types the code shown in the
// terminal, consents at DNSimple, and the callback completes the LinkAccount
// Business Transaction.
package bridge

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"time"

	"dnsshimple/core/identity"
)

type LinkBridge struct {
	Identity *identity.Component
	Attempts *attemptLimiter // optional; Router defaults it
}

func (bridge LinkBridge) Router() *http.ServeMux {
	if bridge.Attempts == nil {
		bridge.Attempts = newAttemptLimiter(10, 10*time.Minute)
	}
	router := http.NewServeMux()
	router.HandleFunc("GET /link", bridge.showLinkForm)
	router.HandleFunc("POST /link", bridge.limited(bridge.acceptLinkCode))
	router.HandleFunc("GET /callback", bridge.limited(bridge.finishOAuth))
	return router
}

// limited guards the two endpoints that consume or complete link codes.
func (bridge LinkBridge) limited(next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		address := clientIP(request)
		if !bridge.Attempts.allow(address) {
			slog.WarnContext(request.Context(), "link attempts rate limited", "client_ip", address)
			writePage(response, http.StatusTooManyRequests,
				"<h1>Too many attempts</h1><p>Wait a few minutes and try again.</p>")
			return
		}
		next(response, request)
	}
}

func (bridge LinkBridge) showLinkForm(response http.ResponseWriter, _ *http.Request) {
	writePage(response, http.StatusOK, `
		<h1>Link your terminal</h1>
		<p>Type the code shown in your SSH session.</p>
		<form method="post" action="/link">
			<input name="code" placeholder="XXXX-XXXX" autofocus autocomplete="off">
			<button type="submit">Continue</button>
		</form>`)
}

func (bridge LinkBridge) acceptLinkCode(response http.ResponseWriter, request *http.Request) {
	typedCode := request.FormValue("code")
	if !bridge.Identity.HasPendingLink(request.Context(), typedCode) {
		writePage(response, http.StatusNotFound,
			"<h1>Unknown or expired code</h1><p>Check your terminal and try again.</p>")
		return
	}
	http.Redirect(response, request,
		bridge.Identity.AuthorizationPageURL(identity.NormalizeLinkCode(typedCode)), http.StatusFound)
}

func (bridge LinkBridge) finishOAuth(response http.ResponseWriter, request *http.Request) {
	linkCode := request.URL.Query().Get("state")
	grantCode := request.URL.Query().Get("code")

	completed := bridge.Identity.CompleteLink(request.Context(), linkCode, grantCode)
	if completed.Failed() {
		// The BTX already logged the cause with its context; here we only
		// choose the page.
		slog.InfoContext(request.Context(), "link handshake rejected at the bridge")
		writePage(response, http.StatusBadGateway,
			"<h1>The handshake failed</h1><p>Reconnect over SSH for a fresh code and try again.</p>")
		return
	}
	writePage(response, http.StatusOK, fmt.Sprintf(
		"<h1>Linked ✓</h1><p>Signed in as <strong>%s</strong>. Return to your terminal.</p>",
		html.EscapeString(completed.Value().Email)))
}

func writePage(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	fmt.Fprintf(response, `<!doctype html><meta name="viewport" content="width=device-width">
		<style>body{font-family:monospace;max-width:32rem;margin:4rem auto;padding:0 1rem}
		input,button{font:inherit;padding:.4rem}</style>%s`, body)
}
