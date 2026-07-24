package sshui

import (
	"dnsshimple/core/billing"
	"dnsshimple/core/insights"
	"dnsshimple/core/registrar"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type chargesLoadedMessage struct {
	charges []billing.Charge
	err     error
}

func (model sessionModel) loadCharges() tea.Cmd {
	component, session, ctx := model.components.Billing, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.Charges(ctx, session)
		if outcome.Failed() {
			return chargesLoadedMessage{err: outcome.Error()}
		}
		return chargesLoadedMessage{charges: outcome.Value()}
	}
}

type analyticsLoadedMessage struct {
	rows []insights.VolumeRow
	err  error
}

type tldsLoadedMessage struct {
	tlds []registrar.TLD
	err  error
}

type tldPriceMessage struct {
	tld   string
	price float64
	err   error
}

func (model sessionModel) loadAnalytics() tea.Cmd {
	component, session, ctx := model.components.Insights, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.VolumeByZone(ctx, session)
		if outcome.Failed() {
			return analyticsLoadedMessage{err: outcome.Error()}
		}
		rows := outcome.Value()
		sort.Slice(rows, func(left, right int) bool { return rows[left].Volume > rows[right].Volume })
		return analyticsLoadedMessage{rows: rows}
	}
}

func (model sessionModel) fetchTLDPrice(tld string) tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.Price(ctx, session, "example."+tld)
		return tldPriceMessage{tld: tld, price: outcome.Value(), err: outcome.Error()}
	}
}

func (model sessionModel) loadTLDAttributes(tld string) tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.ExtendedAttributes(ctx, session, tld)
		if outcome.Failed() {
			return textLoadedMessage{err: outcome.Error()}
		}
		lines := []string{}
		for _, attribute := range outcome.Value() {
			demand := "optional"
			if attribute.Required {
				demand = "required"
			}
			lines = append(lines, fmt.Sprintf("%s (%s) - %s", attribute.Name, demand, attribute.Description))
			for _, option := range attribute.Options {
				lines = append(lines, fmt.Sprintf("    %-8s %s", option.Value, option.Title))
			}
			lines = append(lines, "")
		}
		if len(lines) == 0 {
			lines = []string{"." + tld + " demands no registry-specific attributes."}
		}
		return textLoadedMessage{
			title: "." + tld + " · extended attributes",
			text:  strings.Join(lines, "\n"),
			back:  screenTLDs,
		}
	}
}

func (model sessionModel) loadTLDs() tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.TLDs(ctx, session)
		if outcome.Failed() {
			return tldsLoadedMessage{err: outcome.Error()}
		}
		return tldsLoadedMessage{tlds: outcome.Value()}
	}
}

func (model sessionModel) filteredVolumeRows() []insights.VolumeRow {
	return filterItems(model, model.browser.volumeRows, func(row insights.VolumeRow) string { return row.ZoneName })
}

func (model sessionModel) filteredCharges() []billing.Charge {
	return filterItems(model, model.browser.chargeList, func(charge billing.Charge) string {
		return charge.Reference + " " + charge.State
	})
}

func (model sessionModel) analyticsKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.analyticsCursor = clamp(browser.analyticsCursor-1, len(model.filteredVolumeRows()))
	case "down", "j":
		browser.analyticsCursor = clamp(browser.analyticsCursor+1, len(model.filteredVolumeRows()))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadAnalytics(), spinnerTick())
	}
	return model, nil
}

// fuzzyMatch is a case-insensitive subsequence match: "dv" finds "dev".
func (model sessionModel) filteredTLDs() []registrar.TLD {
	query := model.filterQuery()
	if query == "" {
		return model.browser.tldList
	}
	var matched []registrar.TLD
	for _, tld := range model.browser.tldList {
		if fuzzyMatch(query, tld.Name) {
			matched = append(matched, tld)
		}
	}
	return matched
}

func (model sessionModel) tldsKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visible := model.filteredTLDs()

	switch key.String() {
	case "q":
		return model, tea.Quit
	case "enter":
		if len(visible) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.fetchTLDPrice(visible[browser.tldCursor].Name), spinnerTick())
	case "a":
		if len(visible) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadTLDAttributes(visible[browser.tldCursor].Name), spinnerTick())
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.tldCursor = clamp(browser.tldCursor-1, len(visible))
	case "down", "j":
		browser.tldCursor = clamp(browser.tldCursor+1, len(visible))
	case "pgup":
		browser.tldCursor = clamp(browser.tldCursor-model.visibleRows(), len(visible))
	case "pgdown":
		browser.tldCursor = clamp(browser.tldCursor+model.visibleRows(), len(visible))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadTLDs(), spinnerTick())
	}
	return model, nil
}

func (model sessionModel) billingKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadCharges(), spinnerTick())
	}
	return model, nil
}

func (model sessionModel) viewAnalytics() string {
	browser := model.browser
	lines := []string{model.header("dns analytics · query volume by zone"), ""}
	rowsListed := model.filteredVolumeRows()
	if len(rowsListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no data to show"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.analyticsCursor, len(rowsListed), visible)
	for index := start; index < end; index++ {
		row := rowsListed[index]
		line := fmt.Sprintf("%-57.57s %12d", row.ZoneName, row.Volume)
		prefix := "  "
		if index == browser.analyticsCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
}

func (model sessionModel) viewTLDs() string {
	browser := model.browser
	lines := []string{model.header("tld catalog"), ""}
	catalog := model.filteredTLDs()
	flag := func(enabled bool, label string) string {
		if enabled {
			return label
		}
		return "-"
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.tldCursor, len(catalog), visible)
	for index := start; index < end; index++ {
		tld := catalog[index]
		line := fmt.Sprintf(".%-28s %-9s %-9s %-8s %-4s min %dy",
			tld.Name,
			flag(tld.RegistrationEnabled, "register"),
			flag(tld.TransferEnabled, "transfer"),
			flag(tld.WhoisPrivacy, "privacy"),
			flag(tld.IDN, "idn"),
			tld.MinimumYears)
		prefix := "  "
		if index == browser.tldCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	if position := model.positionLine(browser.tldCursor, len(catalog), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
}

func (model sessionModel) viewBilling() string {
	browser := model.browser
	lines := []string{model.header("billing"), ""}
	chargesListed := model.filteredCharges()
	if len(chargesListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no charges to show"))
	}
	for _, charge := range chargesListed {
		lines = append(lines, fmt.Sprintf("  %-22s %-12s %10s  %s",
			humanTime(charge.InvoicedAt), charge.State, charge.TotalAmount, charge.Reference))
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
}
