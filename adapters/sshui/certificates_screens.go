package sshui

import (
	"dnsshimple/core/certificates"
	"fmt"
	"sort"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type certificateForm struct {
	inputs  [certFieldCount]textinput.Model
	focused int
}

func newCertificateForm(styles styleSet) certificateForm {
	form := certificateForm{}
	placeholders := [certFieldCount]string{
		"subdomain for the common name (empty = apex)",
		"alternate names, comma-separated (optional)",
		"auto-renew: yes / no",
	}
	for field := range certFieldCount {
		input := textinput.New()
		input.Placeholder = placeholders[field]
		input.CharLimit = 253
		input.Width = 44
		input.Prompt = "  "
		input.PromptStyle = styles.selected
		form.inputs[field] = input
	}
	form.inputs[certFieldAutoRenew].SetValue("yes")
	form.focus(0)
	return form
}

type certificatesLoadedMessage struct {
	domainName   string
	certificates []certificates.Certificate
	err          error
}

type certificateRequestedMessage struct {
	err error
}

func (model sessionModel) loadCertificates(domainName string) tea.Cmd {
	component, session, ctx := model.components.Certificates, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.Certificates(ctx, session, domainName)
		if outcome.Failed() {
			return certificatesLoadedMessage{domainName: domainName, err: outcome.Error()}
		}
		return certificatesLoadedMessage{domainName: domainName, certificates: outcome.Value()}
	}
}

func (model sessionModel) issueCertificate() tea.Cmd {
	component, session, ctx := model.components.Certificates, model.session, model.appCtx
	domainName := model.browser.currentDomain
	request, err := model.browser.certForm.request()
	if err != nil {
		return func() tea.Msg { return certificateRequestedMessage{err: err} }
	}
	return func() tea.Msg {
		outcome := component.IssueCertificate(ctx, session, domainName, request)
		return certificateRequestedMessage{err: outcome.Error()}
	}
}

type certificateOverview struct {
	Domain      string
	Certificate certificates.Certificate
}

type allCertificatesLoadedMessage struct {
	items []certificateOverview
	err   error
}

func (model sessionModel) loadAllCertificates() tea.Cmd {
	domainsComponent, certificatesComponent := model.components.Domains, model.components.Certificates
	session, ctx := model.session, model.appCtx
	return func() tea.Msg {
		portfolio := domainsComponent.Domains(ctx, session)
		if portfolio.Failed() {
			return allCertificatesLoadedMessage{err: portfolio.Error()}
		}
		var items []certificateOverview
		for _, domain := range portfolio.Value() {
			listed := certificatesComponent.Certificates(ctx, session, domain.Name)
			if listed.Failed() {
				return allCertificatesLoadedMessage{err: listed.Error()}
			}
			for _, certificate := range listed.Value() {
				items = append(items, certificateOverview{Domain: domain.Name, Certificate: certificate})
			}
		}
		sort.Slice(items, func(left, right int) bool {
			if items[left].Domain != items[right].Domain {
				return items[left].Domain < items[right].Domain
			}
			return items[left].Certificate.ID < items[right].Certificate.ID
		})
		return allCertificatesLoadedMessage{items: items}
	}
}

func (model sessionModel) loadCertificatePEM(domainName string, certificate certificates.Certificate, back appScreen) tea.Cmd {
	component, session, ctx := model.components.Certificates, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.Download(ctx, session, domainName, certificate.ID)
		return textLoadedMessage{title: certificate.CommonName + " · certificate chain",
			text: outcome.Value(), back: back, err: outcome.Error()}
	}
}

func (model sessionModel) loadCertificateKey(domainName string, certificate certificates.Certificate, back appScreen) tea.Cmd {
	component, session, ctx := model.components.Certificates, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.PrivateKey(ctx, session, domainName, certificate.ID)
		return textLoadedMessage{title: certificate.CommonName + " · PRIVATE KEY",
			text: outcome.Value(), back: back, err: outcome.Error(), secret: true}
	}
}

// An action is one entry of a screen's interactive footer: the key the
// handler already understands, and its label.
func (model sessionModel) filteredCertificates() []certificates.Certificate {
	return filterItems(model, model.browser.certificateList, func(certificate certificates.Certificate) string {
		return certificate.CommonName + " " + certificate.State
	})
}

func (model sessionModel) certificatesKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visibleCerts := model.filteredCertificates()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenDomains
		browser.status = ""
	case "up", "k":
		browser.certificateCursor = clamp(browser.certificateCursor-1, len(visibleCerts))
	case "down", "j":
		browser.certificateCursor = clamp(browser.certificateCursor+1, len(visibleCerts))
	case "pgup":
		browser.certificateCursor = clamp(browser.certificateCursor-model.visibleRows(), len(visibleCerts))
	case "pgdown":
		browser.certificateCursor = clamp(browser.certificateCursor+model.visibleRows(), len(visibleCerts))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadCertificates(browser.currentDomain), spinnerTick())
	case "i":
		browser.certForm = newCertificateForm(model.styles)
		browser.screen = screenCertificateForm
		browser.status = ""
		return model, textinput.Blink
	case "R":
		if len(visibleCerts) == 0 {
			return model, nil
		}
		certificate := visibleCerts[browser.certificateCursor]
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.renewCertificate(certificate), spinnerTick())
	case "w":
		if len(visibleCerts) == 0 {
			return model, nil
		}
		browser.loading = true
		return model, tea.Batch(model.loadCertificatePEM(browser.currentDomain,
			visibleCerts[browser.certificateCursor], screenCertificates), spinnerTick())
	case "K":
		if len(visibleCerts) == 0 {
			return model, nil
		}
		certificate := visibleCerts[browser.certificateCursor]
		browser.confirmPrompt = "Show the PRIVATE KEY of " + certificate.CommonName + " on screen?"
		browser.confirmCommand = model.loadCertificateKey(browser.currentDomain, certificate, screenCertificates)
		browser.confirmReturn = screenCertificates
		browser.screen = screenConfirmAction
		return model, nil
	}
	return model, nil
}

func (model sessionModel) certificateFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenCertificates
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.issueCertificate(), spinnerTick())
	case "tab", "down":
		browser.certForm.focus(browser.certForm.focused + 1)
		return model, nil
	case "shift+tab", "up":
		browser.certForm.focus(browser.certForm.focused - 1)
		return model, nil
	}
	var inputCommand tea.Cmd
	browser.certForm.inputs[browser.certForm.focused], inputCommand =
		browser.certForm.inputs[browser.certForm.focused].Update(key)
	return model, inputCommand
}

func (model sessionModel) allCertificatesKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visible := model.filteredAllCerts()

	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.allCertCursor = clamp(browser.allCertCursor-1, len(visible))
	case "down", "j":
		browser.allCertCursor = clamp(browser.allCertCursor+1, len(visible))
	case "pgup":
		browser.allCertCursor = clamp(browser.allCertCursor-model.visibleRows(), len(visible))
	case "pgdown":
		browser.allCertCursor = clamp(browser.allCertCursor+model.visibleRows(), len(visible))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadAllCertificates(), spinnerTick())
	case "w":
		if len(visible) == 0 {
			return model, nil
		}
		item := visible[browser.allCertCursor]
		browser.loading = true
		return model, tea.Batch(model.loadCertificatePEM(item.Domain, item.Certificate, screenAllCertificates), spinnerTick())
	case "K":
		if len(visible) == 0 {
			return model, nil
		}
		item := visible[browser.allCertCursor]
		browser.confirmPrompt = "Show the PRIVATE KEY of " + item.Certificate.CommonName + " on screen?"
		browser.confirmCommand = model.loadCertificateKey(item.Domain, item.Certificate, screenAllCertificates)
		browser.confirmReturn = screenAllCertificates
		browser.screen = screenConfirmAction
		return model, nil
	case "i":
		// Issuing needs a domain: pick one, then the usual form.
		if len(browser.domainList) > 0 {
			browser.screen = screenCertDomainPicker
			browser.status = ""
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadDomains(), spinnerTick())
	}
	return model, nil
}

func (model sessionModel) certDomainPickerKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visibleDomains := model.filteredDomains()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenAllCertificates
		browser.status = ""
	case "up", "k":
		browser.certDomainCursor = clamp(browser.certDomainCursor-1, len(visibleDomains))
	case "down", "j":
		browser.certDomainCursor = clamp(browser.certDomainCursor+1, len(visibleDomains))
	case "enter":
		if len(visibleDomains) == 0 {
			return model, nil
		}
		browser.currentDomain = visibleDomains[browser.certDomainCursor].Name
		browser.certForm = newCertificateForm(model.styles)
		browser.screen = screenCertificateForm
		browser.status = ""
		return model, textinput.Blink
	}
	return model, nil
}

func (model sessionModel) viewAllCertificates() string {
	browser := model.browser
	lines := []string{model.header("certificates"), ""}
	items := model.filteredAllCerts()
	if len(items) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  none across the portfolio; i requests one"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.allCertCursor, len(items), visible)
	for index := start; index < end; index++ {
		item := items[index]
		line := fmt.Sprintf("%-32.32s %-12s expires %s",
			item.Certificate.CommonName, item.Certificate.State, humanTime(item.Certificate.ExpiresAt))
		prefix := "  "
		if index == browser.allCertCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	if position := model.positionLine(browser.allCertCursor, len(items), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
}

func (model sessionModel) viewCertDomainPicker() string {
	browser := model.browser
	lines := []string{model.header("issue a certificate for..."), ""}
	visible := model.visibleRows()
	pickerDomains := model.filteredDomains()
	start, end := listWindow(browser.certDomainCursor, len(pickerDomains), visible)
	for index := start; index < end; index++ {
		line := pickerDomains[index].Name
		prefix := "  "
		if index == browser.certDomainCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
}

func (model sessionModel) viewCertificateForm() string {
	browser := model.browser
	labels := [certFieldCount]string{"Name       ", "Alt names  ", "Auto-renew "}
	lines := []string{model.header(browser.currentDomain + " · request certificate"), ""}
	for field := range certFieldCount {
		lines = append(lines, "  "+labels[field]+" "+browser.certForm.inputs[field].View())
	}
	lines = append(lines, "",
		model.styles.subtle.Render("  Let's Encrypt; issuance polls in the background"))
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("tab next field · enter request · esc cancel"))...)
}

func (model sessionModel) viewCertificates() string {
	browser := model.browser
	lines := []string{model.header(browser.currentDomain + " · certificates"), ""}
	certsListed := model.filteredCertificates()
	if len(certsListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no certificates to show; i issues one"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.certificateCursor, len(certsListed), visible)
	for index := start; index < end; index++ {
		certificate := certsListed[index]
		line := fmt.Sprintf("%-30.30s %-12s %-10s expires %s",
			certificate.CommonName, certificate.State,
			autoRenewLabel(certificate.AutoRenew), humanTime(certificate.ExpiresAt))
		cursor := "  "
		if index == browser.certificateCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.certificateCursor, len(certsListed), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
}

func (model sessionModel) filteredAllCerts() []certificateOverview {
	query := model.filterQuery()
	if query == "" {
		return model.browser.allCertList
	}
	var matched []certificateOverview
	for _, item := range model.browser.allCertList {
		if fuzzyMatch(query, item.Domain+" "+item.Certificate.CommonName) {
			matched = append(matched, item)
		}
	}
	return matched
}

type certificateRenewedMessage struct {
	commonName string
	err        error
}

func (model sessionModel) renewCertificate(certificate certificates.Certificate) tea.Cmd {
	component, session, ctx := model.components.Certificates, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.RenewCertificate(ctx, session, domainName, certificate.ID, certificate.AutoRenew)
		return certificateRenewedMessage{commonName: certificate.CommonName, err: outcome.Error()}
	}
}
