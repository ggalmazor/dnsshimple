package sshui

import (
	"dnsshimple/core/registrar"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type transferSubmittedMessage struct {
	domainName string
	transfer   registrar.Transfer
	err        error
}

func (model sessionModel) submitRegistration(extendedAttributes map[string]string) tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := strings.ToLower(strings.TrimSpace(model.browser.registerInput.Value()))
	return func() tea.Msg {
		outcome := component.RegisterDomain(ctx, session, domainName, extendedAttributes)
		return registrationSubmittedMessage{domainName: domainName, err: outcome.Error()}
	}
}

func (model sessionModel) submitTransfer(extendedAttributes map[string]string) tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := strings.ToLower(strings.TrimSpace(model.browser.transferForm[0].Value()))
	authCode := model.browser.transferForm[1].Value()
	return func() tea.Msg {
		outcome := component.TransferDomain(ctx, session, domainName, authCode, extendedAttributes)
		return transferSubmittedMessage{domainName: domainName, transfer: outcome.Value(), err: outcome.Error()}
	}
}

type registrantChangedMessage struct {
	err error
}

// loadAllCertificates aggregates certificates across the portfolio - one
// call per domain, absorbed by the 45 s read cache on refreshes.
type quoteMessage struct {
	domainName string
	quote      registrar.Quote
	err        error
}

func (model sessionModel) quoteDomain() tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := strings.ToLower(strings.TrimSpace(model.browser.registerInput.Value()))
	return func() tea.Msg {
		outcome := component.QuoteDomain(ctx, session, domainName)
		return quoteMessage{domainName: domainName, quote: outcome.Value(), err: outcome.Error()}
	}
}

func (model sessionModel) transferFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenDomains
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.submitTransfer(nil), spinnerTick())
	case "tab", "down", "shift+tab", "up":
		browser.transferFocused = (browser.transferFocused + 1) % 2
		for field := range 2 {
			if field == browser.transferFocused {
				browser.transferForm[field].Prompt = "> "
				browser.transferForm[field].Focus()
			} else {
				browser.transferForm[field].Prompt = "  "
				browser.transferForm[field].Blur()
			}
		}
		return model, nil
	}
	var inputCommand tea.Cmd
	browser.transferForm[browser.transferFocused], inputCommand =
		browser.transferForm[browser.transferFocused].Update(key)
	return model, inputCommand
}

func (model sessionModel) registerFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenDomains
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.quoteDomain(), spinnerTick())
	}
	var inputCommand tea.Cmd
	browser.registerInput, inputCommand = browser.registerInput.Update(key)
	return model, inputCommand
}

func (model sessionModel) viewTransferForm() string {
	browser := model.browser
	return lipgloss.JoinVertical(lipgloss.Left,
		model.header("transfer a domain in"),
		"",
		"  Domain     "+browser.transferForm[0].View(),
		"  Auth code  "+browser.transferForm[1].View(),
		"",
		model.styles.subtle.Render("  unlock the domain at the losing registrar first"),
		model.footer("tab next field · enter transfer · esc cancel"),
	)
}

func (model sessionModel) viewRegisterForm() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		model.header("register a domain"),
		"",
		"  Domain  "+model.browser.registerInput.View(),
		"",
		model.styles.subtle.Render("  registers with your account's first contact"),
		model.footer("enter register · esc cancel"),
	)
}

type changeRow struct {
	kind     string // "registrant" or "transfer"
	id       int64
	domain   string
	detail   string
	state    string
	terminal bool
}

func (model sessionModel) changeRows() []changeRow {
	browser := model.browser
	rows := make([]changeRow, 0, len(browser.pendingTransfers)+len(browser.registrantChangeList))
	for _, pending := range browser.pendingTransfers {
		rows = append(rows, changeRow{
			kind: "transfer", id: pending.transfer.ID, domain: pending.domain,
			detail: pending.domain, state: pending.transfer.State,
			terminal: pending.transfer.Terminal(),
		})
	}
	for _, change := range browser.registrantChangeList {
		rows = append(rows, changeRow{
			kind: "registrant", id: change.ID,
			detail:   fmt.Sprintf("domain #%d → contact #%d", change.DomainID, change.ContactID),
			state:    change.State,
			terminal: change.Terminal(),
		})
	}
	return rows
}

func (model sessionModel) filteredChangeRows() []changeRow {
	return filterItems(model, model.changeRows(), func(row changeRow) string {
		return row.kind + " " + row.detail + " " + row.state
	})
}

type registrantChangesLoadedMessage struct {
	changes []registrar.RegistrantChange
	err     error
}

func (model sessionModel) extAttrFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	fields := len(browser.extAttrForm)
	switch key.String() {
	case "esc":
		browser.screen = screenDomains
		browser.status = ""
		return model, nil
	case "enter":
		values := map[string]string{}
		for index, attribute := range browser.extAttrDefs {
			value := strings.TrimSpace(browser.extAttrForm[index].Value())
			if value == "" {
				if attribute.Required {
					browser.status = "✗ " + attribute.Name + " is required by the registry"
					return model, nil
				}
				continue
			}
			values[attribute.Name] = value
		}
		browser.loading = true
		browser.status = ""
		if browser.extAttrAction == "transfer" {
			return model, tea.Batch(model.submitTransfer(values), spinnerTick())
		}
		return model, tea.Batch(model.submitRegistration(values), spinnerTick())
	case "tab", "down":
		browser.extAttrFocused = (browser.extAttrFocused + 1) % fields
	case "shift+tab", "up":
		browser.extAttrFocused = (browser.extAttrFocused + fields - 1) % fields
	default:
		var inputCommand tea.Cmd
		browser.extAttrForm[browser.extAttrFocused], inputCommand = browser.extAttrForm[browser.extAttrFocused].Update(key)
		return model, inputCommand
	}
	for index := range browser.extAttrForm {
		if index == browser.extAttrFocused {
			browser.extAttrForm[index].Prompt = "> "
			browser.extAttrForm[index].Focus()
		} else {
			browser.extAttrForm[index].Prompt = "  "
			browser.extAttrForm[index].Blur()
		}
	}
	return model, nil
}

func (model sessionModel) viewExtAttrForm() string {
	browser := model.browser
	lines := []string{model.header("registry details · " + browser.extAttrAction), ""}
	for index, attribute := range browser.extAttrDefs {
		label := attribute.Title
		if label == "" {
			label = attribute.Name
		}
		if attribute.Required {
			label += " *"
		}
		lines = append(lines, "  "+label)
		lines = append(lines, "  "+browser.extAttrForm[index].View())
	}
	lines = append(lines, "", model.styles.subtle.Render("  * required by the registry"))
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("tab next field · enter submit · esc cancel"))...)
}

type domainMutatedMessage struct {
	verb string
	name string
	err  error
}

func (model sessionModel) addDomain() tea.Cmd {
	component, session, ctx := model.components.Domains, model.session, model.appCtx
	domainName := strings.ToLower(strings.TrimSpace(model.browser.registerInput.Value()))
	return func() tea.Msg {
		outcome := component.AddDomain(ctx, session, domainName)
		return domainMutatedMessage{verb: "added", name: domainName, err: outcome.Error()}
	}
}

func (model sessionModel) deleteDomain(domainName string) tea.Cmd {
	component, session, ctx := model.components.Domains, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.DeleteDomain(ctx, session, domainName)
		return domainMutatedMessage{verb: "removed", name: domainName, err: outcome.Error()}
	}
}

func (model sessionModel) addDomainFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenDomains
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.addDomain(), spinnerTick())
	}
	var inputCommand tea.Cmd
	browser.registerInput, inputCommand = browser.registerInput.Update(key)
	return model, inputCommand
}

func (model sessionModel) viewAddDomainForm() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		model.header("add a domain"),
		"",
		"  Domain  "+model.browser.registerInput.View(),
		"",
		model.styles.subtle.Render("  hosts DNS for a domain you already own - no registration happens"),
		model.footer("enter add · esc cancel"),
	)
}

func (model sessionModel) loadRegistrantChanges() tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.RegistrantChanges(ctx, session)
		if outcome.Failed() {
			return registrantChangesLoadedMessage{err: outcome.Error()}
		}
		return registrantChangesLoadedMessage{changes: outcome.Value()}
	}
}

func (model sessionModel) cancelChange(row changeRow) tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	return func() tea.Msg {
		if row.kind == "transfer" {
			outcome := component.CancelTransfer(ctx, session, row.domain, row.id)
			return changeCancelledMessage{kind: row.kind, id: row.id, err: outcome.Error()}
		}
		outcome := component.CancelRegistrantChange(ctx, session, row.id)
		return changeCancelledMessage{kind: row.kind, id: row.id, err: outcome.Error()}
	}
}

func (model sessionModel) registrantChangesKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visibleRows := model.filteredChangeRows()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenDomains
		browser.status = ""
	case "up", "k":
		browser.registrantChangeCursor = clamp(browser.registrantChangeCursor-1, len(visibleRows))
	case "down", "j":
		browser.registrantChangeCursor = clamp(browser.registrantChangeCursor+1, len(visibleRows))
	case "pgup":
		browser.registrantChangeCursor = clamp(browser.registrantChangeCursor-model.visibleRows(), len(visibleRows))
	case "pgdown":
		browser.registrantChangeCursor = clamp(browser.registrantChangeCursor+model.visibleRows(), len(visibleRows))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadRegistrantChanges(), spinnerTick())
	case "x":
		if len(visibleRows) == 0 {
			return model, nil
		}
		row := visibleRows[browser.registrantChangeCursor]
		if row.terminal {
			browser.status = fmt.Sprintf("%s #%d is already %s", row.kind, row.id, row.state)
			return model, nil
		}
		browser.confirmPrompt = fmt.Sprintf("Cancel %s #%d (%s)?", row.kind, row.id, row.detail)
		browser.confirmCommand = model.cancelChange(row)
		browser.confirmReturn = screenRegistrantChanges
		browser.screen = screenConfirmAction
	}
	return model, nil
}

func (model sessionModel) viewRegistrantChanges() string {
	browser := model.browser
	lines := []string{model.header("changes"), ""}
	rowsListed := model.filteredChangeRows()
	if len(rowsListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  none in flight; transfers started this session and registrant changes land here"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.registrantChangeCursor, len(rowsListed), visible)
	for index := start; index < end; index++ {
		row := rowsListed[index]
		line := fmt.Sprintf("%-11s #%-6d %-38.38s %s", row.kind, row.id, row.detail, row.state)
		prefix := "  "
		if index == browser.registrantChangeCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
}

// openDemandsForm renders the registry's form definition carried by the
// Registrar's typed refusal; the TUI knows nothing about where demands
// come from.
func (model sessionModel) openDemandsForm(action string, demands []registrar.ExtendedAttribute) (tea.Model, tea.Cmd) {
	browser := &model.browser
	browser.loading = false
	browser.extAttrDefs = demands
	browser.extAttrAction = action
	browser.extAttrForm = make([]textinput.Model, len(demands))
	for index, attribute := range demands {
		input := textinput.New()
		hint := attribute.Description
		if len(attribute.Options) > 0 {
			values := make([]string, 0, len(attribute.Options))
			for _, option := range attribute.Options {
				values = append(values, option.Value)
			}
			hint = "one of " + strings.Join(values, ", ")
		}
		input.Placeholder = hint
		input.CharLimit = 255
		input.Width = 44
		input.Prompt = "  "
		input.PromptStyle = model.styles.selected
		browser.extAttrForm[index] = input
	}
	if len(browser.extAttrForm) > 0 {
		browser.extAttrForm[0].Prompt = "> "
		browser.extAttrForm[0].Focus()
	}
	browser.extAttrFocused = 0
	browser.screen = screenExtAttrForm
	return model, textinput.Blink
}
