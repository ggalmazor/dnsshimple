package sshui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"dnsshimple/core/identity"
)

// The M1 TUI has three states: waiting for the handshake, linked, and failed.
// The real asset screens (zones, domains, certificates) arrive with M2+.

type sessionReadyMessage identity.Session
type linkFailedMessage struct{ cause error }
type spinnerTickMessage time.Time

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var (
	frameStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 3)
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	codeStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).Padding(0, 1)
	errorStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
	subtleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

type screenState int

const (
	stateWaiting screenState = iota
	stateLinked
	stateFailed
)

type waitingScreen struct {
	identity    *identity.Component
	ctx         context.Context
	fingerprint string
	linkPageURL string
	pending     identity.PendingLink
}

type sessionModel struct {
	state        screenState
	fingerprint  string
	waiting      *waitingScreen
	session      identity.Session
	failure      string
	spinnerFrame int
}

func newWaitingModel(screen waitingScreen) sessionModel {
	return sessionModel{state: stateWaiting, fingerprint: screen.fingerprint, waiting: &screen}
}

func newLinkedModel(fingerprint string, session identity.Session) sessionModel {
	return sessionModel{state: stateLinked, fingerprint: fingerprint, session: session}
}

func newFailedModel(failure string) sessionModel {
	return sessionModel{state: stateFailed, failure: failure}
}

func (model sessionModel) Init() tea.Cmd {
	if model.state != stateWaiting {
		return nil
	}
	return tea.Batch(awaitHandshake(*model.waiting), spinnerTick())
}

// awaitHandshake parks on the Component (LISTEN/NOTIFY underneath) and turns
// the wake into a message. The wait dies with the link code's own lifetime.
func awaitHandshake(screen waitingScreen) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(screen.ctx, identity.LinkCodeLifetime)
		defer cancel()
		outcome := screen.identity.AwaitLink(ctx, screen.pending.Code, screen.fingerprint)
		if outcome.Failed() {
			return linkFailedMessage{cause: outcome.Error()}
		}
		return sessionReadyMessage(outcome.Value())
	}
}

func spinnerTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(now time.Time) tea.Msg {
		return spinnerTickMessage(now)
	})
}

func (model sessionModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyMsg:
		switch message.String() {
		case "q", "ctrl+c", "esc":
			return model, tea.Quit
		}
	case sessionReadyMessage:
		model.state = stateLinked
		model.session = identity.Session(message)
		return model, nil
	case linkFailedMessage:
		model.state = stateFailed
		model.failure = "The link timed out or failed. Reconnect for a fresh code."
		return model, nil
	case spinnerTickMessage:
		if model.state == stateWaiting {
			model.spinnerFrame = (model.spinnerFrame + 1) % len(spinnerFrames)
			return model, spinnerTick()
		}
	}
	return model, nil
}

func (model sessionModel) View() string {
	switch model.state {
	case stateWaiting:
		return frameStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("DNSSHIMPLE"),
			"",
			"Link your DNSimple account:",
			"",
			"  1. Open   "+model.waiting.linkPageURL,
			"  2. Enter  "+codeStyle.Render(identity.DisplayLinkCode(model.waiting.pending.Code)),
			"",
			spinnerFrames[model.spinnerFrame]+" waiting for the browser handshake...",
			"",
			subtleStyle.Render("key "+model.fingerprint),
			subtleStyle.Render("q to quit"),
		)) + "\n"
	case stateLinked:
		return frameStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("DNSSHIMPLE"),
			"",
			"Linked ✓",
			"",
			fmt.Sprintf("  Account  #%d", model.session.Tenant.AccountID),
			fmt.Sprintf("  Email    %s", model.session.Tenant.Email),
			"",
			subtleStyle.Render("key "+model.fingerprint),
			subtleStyle.Render("q to quit"),
		)) + "\n"
	default:
		return frameStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("DNSSHIMPLE"),
			"",
			errorStyle.Render("✗ "+model.failure),
			"",
			subtleStyle.Render("q to quit"),
		)) + "\n"
	}
}
