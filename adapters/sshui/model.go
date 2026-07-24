package sshui

import (
	"context"
	"math/rand/v2"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"dnsshimple/core/billing"
	"dnsshimple/core/certificates"
	"dnsshimple/core/contacts"
	"dnsshimple/core/domains"
	"dnsshimple/core/identity"
	"dnsshimple/core/insights"
	"dnsshimple/core/pushes"
	"dnsshimple/core/registrar"
	"dnsshimple/core/secondarydns"
	"dnsshimple/core/services"
	"dnsshimple/core/templates"
	"dnsshimple/core/webhooks"
	"dnsshimple/core/zones"
)

// Components bundles what the TUI drives; the SSH adapter owns no logic.
type Components struct {
	Identity     *identity.Component
	Zones        *zones.Component
	Domains      *domains.Component
	Registrar    *registrar.Component
	Certificates *certificates.Component
	Billing      *billing.Component
	Contacts     *contacts.Component
	Templates    *templates.Component
	Services     *services.Component
	Pushes       *pushes.Component
	Webhooks     *webhooks.Component
	SecondaryDNS *secondarydns.Component
	Insights     *insights.Component
}

// The root model runs two layers: the auth flow (waiting/failed) and, once a
// Session exists, the zones browser (browse.go).

type sessionReadyMessage identity.Session
type linkFailedMessage struct{ cause error }
type spinnerTickMessage time.Time

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// styleSet is built from a per-session renderer: the server process has no
// TTY, so the global lipgloss renderer degrades to no-color and every style
// silently strips - only the SSH session knows the client's capabilities.
type styleSet struct {
	renderer  *lipgloss.Renderer
	frame     lipgloss.Style
	title     lipgloss.Style
	code      lipgloss.Style
	errorText lipgloss.Style
	subtle    lipgloss.Style
	selected  lipgloss.Style
	status    lipgloss.Style
}

func newStyleSet(renderer *lipgloss.Renderer) styleSet {
	return styleSet{
		renderer: renderer,
		frame: renderer.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 3),
		title:     renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("63")),
		code:      renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).Padding(0, 1),
		errorText: renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("203")),
		subtle:    renderer.NewStyle().Foreground(lipgloss.Color("241")),
		selected:  renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("212")),
		status:    renderer.NewStyle().Foreground(lipgloss.Color("214")),
	}
}

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
	// auth layer
	state        screenState
	fingerprint  string
	linkPageURL  string
	waiting      *waitingScreen
	session      identity.Session
	failure      string
	spinnerFrame int

	// browser layer (browse.go)
	appCtx     context.Context
	components Components
	browser    browserState

	// terminal geometry, for centering and list windowing
	width  int
	height int

	styles  styleSet
	artwork string // Trusty, rendered once per session
	logoArt string // the DNSimple logomark for the frame corner
}

func newWaitingModel(screen waitingScreen, components Components, styles styleSet) sessionModel {
	return sessionModel{
		state:       stateWaiting,
		fingerprint: screen.fingerprint,
		linkPageURL: screen.linkPageURL,
		waiting:     &screen,
		appCtx:      screen.ctx,
		components:  components,
		browser:     newBrowserState(),
		styles:      styles,
		artwork:     renderTrusty(styles.renderer),
		logoArt:     renderWordmark(styles.renderer),
	}
}

func newLinkedModel(ctx context.Context, fingerprint string, session identity.Session, components Components, styles styleSet, linkPageURL string) sessionModel {
	return sessionModel{
		state:       stateLinked,
		fingerprint: fingerprint,
		linkPageURL: linkPageURL,
		session:     session,
		appCtx:      ctx,
		components:  components,
		browser:     newBrowserState(),
		styles:      styles,
		artwork:     renderTrusty(styles.renderer),
		logoArt:     renderWordmark(styles.renderer),
	}
}

func newFailedModel(failure string, styles styleSet) sessionModel {
	return sessionModel{state: stateFailed, failure: failure, styles: styles}
}

func (model sessionModel) Init() tea.Cmd {
	switch model.state {
	case stateWaiting:
		return tea.Batch(awaitHandshake(*model.waiting), spinnerTick())
	default:
		return nil
	}
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
	case tea.WindowSizeMsg:
		model.width, model.height = message.Width, message.Height
		return model, nil
	case tea.KeyMsg:
		if message.String() == "ctrl+c" {
			return model, tea.Quit
		}
		if model.state == stateLinked {
			return model.updateBrowser(message)
		}
		if message.String() == "q" || message.String() == "esc" {
			return model, tea.Quit
		}
	case sessionReadyMessage:
		model.state = stateLinked
		fresh := identity.Session(message)
		// A handshake landing mid-session switches accounts: every cached
		// list belongs to the old tenant, so the browser resets (the party
		// survives - it is a session mood, not account data).
		if model.session.Tenant.ID != 0 && model.session.Tenant.ID != fresh.Tenant.ID {
			party := model.browser.partyMode
			model.browser = newBrowserState()
			model.browser.partyMode = party
			model.browser.status = "✓ linked " + accountLabel(fresh.Tenant)
		}
		model.session = fresh
		model.waiting = nil
		return model, nil
	case linkFailedMessage:
		model.state = stateFailed
		model.failure = "The link timed out or failed. Reconnect for a fresh code."
		return model, nil
	case spinnerTickMessage:
		model.spinnerFrame = (model.spinnerFrame + 1) % len(spinnerFrames)
		if model.state == stateWaiting || model.browser.loading {
			return model, spinnerTick()
		}
		return model, nil
	default:
		if model.state == stateLinked {
			return model.updateBrowser(message)
		}
	}
	return model, nil
}

func (model sessionModel) View() string {
	content := model.renderScreen()
	if model.width > 0 && model.height > 0 {
		return lipgloss.Place(model.width, model.height, lipgloss.Center, lipgloss.Center, content)
	}
	return content
}

const (
	minFrameWidth  = 72
	minFrameHeight = 26
)

// frameHeight is fixed per terminal: the frame never stretches for content;
// lists scroll inside it instead.
func (model sessionModel) frameHeight() int {
	if model.height > 6 && model.height-6 < minFrameHeight {
		return model.height - 6
	}
	return minFrameHeight
}

// partyPalette holds the 256-color stops party mode draws from.
var partyPalette = []lipgloss.Color{"196", "208", "220", "46", "51", "39", "129", "201"}

func partyColor() lipgloss.Color {
	return partyPalette[rand.IntN(len(partyPalette))]
}

// partyTint tints every body line with a random party color; segments a
// screen already styled keep their own color, so selections stay readable.
func partyTint(renderer *lipgloss.Renderer, body string) string {
	lines := strings.Split(body, "\n")
	for index, line := range lines {
		lines[index] = renderer.NewStyle().Foreground(partyColor()).Render(line)
	}
	return strings.Join(lines, "\n")
}

// framed pads every screen to one consistent fixed size and fills the empty
// space with a low-key dot pattern through the session renderer.
func (model sessionModel) framed(body string) string {
	dotColor := lipgloss.Color("237")
	if model.browser.partyMode {
		body = partyTint(model.styles.renderer, body)
		dotColor = partyColor()
	}
	width := max(minFrameWidth, lipgloss.Width(body))
	if model.width > 10 {
		width = min(width, model.width-10)
	}
	height := max(model.frameHeight(), lipgloss.Height(body))
	dots := []lipgloss.WhitespaceOption{
		lipgloss.WithWhitespaceChars(" ·"),
		lipgloss.WithWhitespaceForeground(dotColor),
	}
	bodyHeight := lipgloss.Height(body)
	placed := model.styles.renderer.Place(width, bodyHeight, lipgloss.Left, lipgloss.Top, body, dots...)
	// The logomark lives in the bottom-right of the background fill - only
	// when enough empty rows exist, so it never crowds a long list.
	if filler := height - bodyHeight; filler > 0 {
		corner := ""
		if model.logoArt != "" && filler >= lipgloss.Height(model.logoArt) {
			corner = model.logoArt
			// The prebuilt wordmark carries its own dim style, which
			// survives the per-line tint - party mode redraws it.
			if model.browser.partyMode {
				corner = renderWordmarkColors(model.styles.renderer,
					func(int) lipgloss.Color { return partyColor() }, dotColor)
			}
		}
		placed = lipgloss.JoinVertical(lipgloss.Left, placed,
			model.styles.renderer.Place(width, filler, lipgloss.Right, lipgloss.Bottom, corner, dots...))
	}
	return model.styles.frame.Render(placed) + "\n"
}

func (model sessionModel) renderScreen() string {
	switch model.state {
	case stateWaiting:
		return model.framed(lipgloss.JoinVertical(lipgloss.Center,
			model.artwork,
			"",
			lipgloss.JoinVertical(lipgloss.Left,
				model.styles.title.Render("DNSSHIMPLE"),
				"",
				"Link your DNSimple account:",
				"",
				"  1. Open   "+model.waiting.linkPageURL,
				"  2. Enter  "+model.styles.code.Render(identity.DisplayLinkCode(model.waiting.pending.Code)),
				"",
				spinnerFrames[model.spinnerFrame]+" waiting for the browser handshake...",
				"",
				model.styles.subtle.Render("key "+model.fingerprint),
				model.styles.subtle.Render("q to quit"),
			))) + "\n"
	case stateLinked:
		return model.viewBrowser()
	default:
		return model.framed(lipgloss.JoinVertical(lipgloss.Left,
			model.styles.title.Render("DNSSHIMPLE"),
			"",
			model.styles.errorText.Render("✗ "+model.failure),
			"",
			model.styles.subtle.Render("q to quit"),
		))
	}
}
