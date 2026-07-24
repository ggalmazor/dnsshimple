package sshui

import (
	"context"
	"io"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"dnsshimple/adapters/memory"
	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
)

// The picker's "link another" must park the linked session back on the
// waiting screen with a fresh code.
func TestLinkAnotherParksTheSessionOnTheWaitingScreen(test *testing.T) {
	repository := memory.NewIdentityRepository()
	component := identity.NewComponent(repository, nil, nil, telemetry.NewMetrics())
	model := sessionModel{
		state:       stateLinked,
		fingerprint: "SHA256:felix",
		linkPageURL: "http://localhost:8181/link",
		appCtx:      context.Background(),
		components:  Components{Identity: component},
		session:     identity.Session{Tenant: identity.Tenant{ID: 1, AccountID: 2416}},
		styles:      newStyleSet(lipgloss.NewRenderer(io.Discard)),
	}
	model.browser = newBrowserState()
	model.browser.screen = screenAccountPicker

	updated, command := model.accountPickerKeys(keyFor("n"))
	model = updated.(sessionModel)
	if command == nil {
		test.Fatal("n must produce a command")
	}
	message := model.beginRelink()()
	afterAny, _ := model.Update(message)
	after := afterAny.(sessionModel)
	if after.state != stateWaiting {
		test.Fatalf("the session must park on the waiting screen; state=%d status=%q", after.state, after.browser.status)
	}
	if after.waiting == nil || after.waiting.pending.Code == "" {
		test.Fatal("the waiting screen must carry a fresh link code")
	}
}

// Same flow, but through the full Update pipeline the terminal drives,
// including the footer preamble and the command the key returns.
func TestLinkAnotherWorksThroughTheFullKeyPipeline(test *testing.T) {
	repository := memory.NewIdentityRepository()
	component := identity.NewComponent(repository, nil, nil, telemetry.NewMetrics())
	model := sessionModel{
		state:       stateLinked,
		fingerprint: "SHA256:felix",
		linkPageURL: "http://localhost:8181/link",
		appCtx:      context.Background(),
		components:  Components{Identity: component},
		session:     identity.Session{Tenant: identity.Tenant{ID: 1, AccountID: 2416}},
		styles:      newStyleSet(lipgloss.NewRenderer(io.Discard)),
	}
	model.browser = newBrowserState()
	model.browser.screen = screenAccountPicker

	afterKeyAny, command := model.Update(keyFor("n"))
	model = afterKeyAny.(sessionModel)
	if command == nil {
		test.Fatal("n through Update must produce a command")
	}
	relink := findRelinkMessage(command())
	if relink == nil {
		test.Fatal("the command tree must contain the relink message")
	}
	afterAny, _ := model.Update(relink)
	after := afterAny.(sessionModel)
	if after.state != stateWaiting {
		test.Fatalf("state=%d status=%q", after.state, after.browser.status)
	}
}

// findRelinkMessage walks a possibly-batched message tree for the relink.
func findRelinkMessage(message tea.Msg) tea.Msg {
	switch typed := message.(type) {
	case relinkStartedMessage:
		return typed
	case tea.BatchMsg:
		for _, command := range typed {
			if command == nil {
				continue
			}
			if found := findRelinkMessage(command()); found != nil {
				return found
			}
		}
	}
	return nil
}
