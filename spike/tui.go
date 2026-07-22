package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The spike TUI has exactly two states: waiting for the browser handshake,
// and showing the linked account. Production replaces this with real screens.

type accountLinkedMessage LinkedAccount
type spinnerTickMessage time.Time

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var (
	frameStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 3)
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	codeStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).Padding(0, 1)
	subtleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

type sessionModel struct {
	fingerprint  string
	linkPageURL  string
	displayCode  string
	completed    chan LinkedAccount
	waiting      bool
	account      LinkedAccount
	spinnerFrame int
}

func newWaitingModel(fingerprint, linkPageURL string, pendingLink *LinkRequest) sessionModel {
	return sessionModel{
		fingerprint: fingerprint,
		linkPageURL: linkPageURL,
		displayCode: DisplayLinkCode(pendingLink.Code),
		completed:   pendingLink.Completed,
		waiting:     true,
	}
}

func newLinkedModel(fingerprint string, account LinkedAccount) sessionModel {
	return sessionModel{fingerprint: fingerprint, account: account}
}

func (model sessionModel) Init() tea.Cmd {
	if !model.waiting {
		return nil
	}
	return tea.Batch(waitForHandshake(model.completed), spinnerTick())
}

// waitForHandshake parks a goroutine on the link channel and turns its
// resolution into a Bubble Tea message.
func waitForHandshake(completed chan LinkedAccount) tea.Cmd {
	return func() tea.Msg {
		return accountLinkedMessage(<-completed)
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
	case accountLinkedMessage:
		model.waiting = false
		model.account = LinkedAccount(message)
		return model, nil
	case spinnerTickMessage:
		if model.waiting {
			model.spinnerFrame = (model.spinnerFrame + 1) % len(spinnerFrames)
			return model, spinnerTick()
		}
	}
	return model, nil
}

func (model sessionModel) View() string {
	if model.waiting {
		return frameStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("DNSSHIMPLE"),
			"",
			"Link your DNSimple account:",
			"",
			"  1. Open   "+model.linkPageURL,
			"  2. Enter  "+codeStyle.Render(model.displayCode),
			"",
			spinnerFrames[model.spinnerFrame]+" waiting for the browser handshake...",
			"",
			subtleStyle.Render("key "+model.fingerprint),
			subtleStyle.Render("q to quit"),
		)) + "\n"
	}
	return frameStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("DNSSHIMPLE"),
		"",
		"Linked ✓",
		"",
		fmt.Sprintf("  Account  #%d", model.account.AccountID),
		fmt.Sprintf("  Email    %s", model.account.Email),
		"",
		subtleStyle.Render("key "+model.fingerprint),
		subtleStyle.Render("q to quit"),
	)) + "\n"
}
