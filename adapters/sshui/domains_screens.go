package sshui

import (
	"dnsshimple/core/domains"
	"fmt"
	"sort"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type domainsLoadedMessage struct {
	domains []domains.Domain
	err     error
}

func (model sessionModel) loadDomains() tea.Cmd {
	component, session, ctx := model.components.Domains, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.Domains(ctx, session)
		if outcome.Failed() {
			return domainsLoadedMessage{err: outcome.Error()}
		}
		listed := outcome.Value()
		sort.Slice(listed, func(left, right int) bool { return listed[left].Name < listed[right].Name })
		return domainsLoadedMessage{domains: listed}
	}
}

func domainSearchText(domain domains.Domain) string {
	return domain.Name + " " + domain.State
}

func (model sessionModel) filteredDomains() []domains.Domain {
	return filterItems(model, model.browser.domainList, domainSearchText)
}

func (model sessionModel) domainsKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visibleDomains := model.filteredDomains()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.domainCursor = clamp(browser.domainCursor-1, len(visibleDomains))
	case "down", "j":
		browser.domainCursor = clamp(browser.domainCursor+1, len(visibleDomains))
	case "pgup":
		browser.domainCursor = clamp(browser.domainCursor-model.visibleRows(), len(visibleDomains))
	case "pgdown":
		browser.domainCursor = clamp(browser.domainCursor+model.visibleRows(), len(visibleDomains))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadDomains(), spinnerTick())
	case "h":
		browser.loading = true
		browser.status = ""
		browser.registrantChangeCursor = 0
		return model, tea.Batch(model.loadRegistrantChanges(), spinnerTick())
	case "a":
		input := textinput.New()
		input.Placeholder = "existing.example - host its DNS here"
		input.CharLimit = 253
		input.Width = 44
		input.Focus()
		browser.registerInput = input
		browser.screen = screenAddDomainForm
		browser.status = ""
		return model, textinput.Blink
	case "d":
		if len(visibleDomains) == 0 {
			return model, nil
		}
		name := visibleDomains[browser.domainCursor].Name
		browser.confirmPrompt = "Remove " + name + " from the account? Its hosted DNS stops resolving; a registration is not cancelled."
		browser.confirmCommand = model.deleteDomain(name)
		browser.confirmReturn = screenDomains
		browser.screen = screenConfirmAction
	case "n":
		input := textinput.New()
		input.Placeholder = "example.com"
		input.CharLimit = 253
		input.Width = 40
		input.Focus()
		browser.registerInput = input
		browser.screen = screenRegisterForm
		browser.status = ""
		return model, textinput.Blink
	case "enter":
		if len(visibleDomains) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		browser.certificateCursor = 0
		return model, tea.Batch(
			model.loadCertificates(visibleDomains[browser.domainCursor].Name), spinnerTick())
	case "s":
		if len(visibleDomains) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		browser.forwardCursor = 0
		return model, tea.Batch(
			model.loadSettings(visibleDomains[browser.domainCursor].Name), spinnerTick())
	case "c":
		// Straight to the certificate form for the selected domain.
		if len(visibleDomains) == 0 {
			return model, nil
		}
		browser.currentDomain = visibleDomains[browser.domainCursor].Name
		browser.certForm = newCertificateForm(model.styles)
		browser.screen = screenCertificateForm
		browser.status = ""
		return model, textinput.Blink
	case "t":
		placeholders := [2]string{"domain to transfer in", "auth code from the losing registrar"}
		for field := range 2 {
			input := textinput.New()
			input.Placeholder = placeholders[field]
			input.CharLimit = 253
			input.Width = 44
			input.Prompt = "  "
			input.PromptStyle = model.styles.selected
			browser.transferForm[field] = input
		}
		// A hosted domain is DNS-only here - the natural transfer-in
		// candidate - so its name prefills and focus jumps to the auth code.
		startField := 0
		if len(visibleDomains) > 0 {
			if selected := visibleDomains[browser.domainCursor]; selected.State == "hosted" {
				browser.transferForm[0].SetValue(selected.Name)
				startField = 1
			}
		}
		browser.transferForm[startField].Prompt = "> "
		browser.transferForm[startField].Focus()
		browser.transferFocused = startField
		browser.screen = screenTransferForm
		browser.status = ""
		return model, textinput.Blink
	}
	return model, nil
}

func (model sessionModel) viewDomains() string {
	browser := model.browser
	lines := []string{model.header("domains"), ""}
	domainsListed := model.filteredDomains()
	if len(domainsListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no domains to show"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.domainCursor, len(domainsListed), visible)
	for index := start; index < end; index++ {
		domain := domainsListed[index]
		line := fmt.Sprintf("%-30.30s %-12s %-10s renews %s",
			domain.Name, domain.State, autoRenewLabel(domain.AutoRenew), humanTime(domain.ExpiresAt))
		cursor := "  "
		if index == browser.domainCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.domainCursor, len(domainsListed), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
}
