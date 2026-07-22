package main

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"

	"dnsshimple/kit"
)

// LinkBridge is the tiny web app carrying the browser leg of the handshake:
// user types the code shown in the terminal, gets bounced through DNSimple
// OAuth, and the callback completes the link.
type LinkBridge struct {
	Identities *IdentityStore
	OAuth      OAuthGateway
}

func (bridge LinkBridge) Router() *http.ServeMux {
	router := http.NewServeMux()
	router.HandleFunc("GET /link", bridge.showLinkForm)
	router.HandleFunc("POST /link", bridge.acceptLinkCode)
	router.HandleFunc("GET /callback", bridge.finishOAuth)
	return router
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
	pendingLink, alive := bridge.Identities.PendingLink(typedCode)
	if !alive {
		writePage(response, http.StatusNotFound,
			"<h1>Unknown or expired code</h1><p>Check your terminal and try again.</p>")
		return
	}
	http.Redirect(response, request,
		bridge.OAuth.BrowserAuthorizationURL(pendingLink.Code), http.StatusFound)
}

func (bridge LinkBridge) finishOAuth(response http.ResponseWriter, request *http.Request) {
	linkCode := request.URL.Query().Get("state")
	grantCode := request.URL.Query().Get("code")

	if _, alive := bridge.Identities.PendingLink(linkCode); !alive {
		writePage(response, http.StatusNotFound,
			"<h1>This link expired</h1><p>Reconnect over SSH for a fresh code.</p>")
		return
	}

	// Railway: redeem the grant, then resolve who the token belongs to.
	linkedIdentity := kit.Chain(
		bridge.OAuth.RedeemAuthorizationCode(grantCode, linkCode),
		bridge.OAuth.FetchIdentity,
	)
	if linkedIdentity.Failed() {
		slog.Error("oauth handshake failed", "link_code", linkCode, "cause", linkedIdentity.Error())
		writePage(response, http.StatusBadGateway,
			"<h1>DNSimple said no</h1><p>The handshake failed. Check the server logs.</p>")
		return
	}

	if err := bridge.Identities.CompleteLink(linkCode, linkedIdentity.Value()); err != nil {
		slog.Error("completing link failed", "link_code", linkCode, "cause", err)
		writePage(response, http.StatusConflict,
			"<h1>Code already used</h1><p>Reconnect over SSH for a fresh code.</p>")
		return
	}

	slog.Info("account linked",
		"account_id", linkedIdentity.Value().AccountID, "email", linkedIdentity.Value().Email)
	writePage(response, http.StatusOK, fmt.Sprintf(
		"<h1>Linked ✓</h1><p>Signed in as <strong>%s</strong>. Return to your terminal.</p>",
		html.EscapeString(linkedIdentity.Value().Email)))
}

func writePage(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	fmt.Fprintf(response, `<!doctype html><meta name="viewport" content="width=device-width">
		<style>body{font-family:monospace;max-width:32rem;margin:4rem auto;padding:0 1rem}
		input,button{font:inherit;padding:.4rem}</style>%s`, body)
}
