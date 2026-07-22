package sshui

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"dnsshimple/core/billing"
	"dnsshimple/core/certificates"
	"dnsshimple/core/domains"
	"dnsshimple/core/gateway"
	"dnsshimple/core/registrar"
	"dnsshimple/core/zones"
)

// The zones browser: list zones, list records, edit in a form, confirm
// deletes. Every operation goes through the zones Component, so validation,
// rate limiting, caching, and the breaker all apply before DNSimple is hit.

type appScreen int

const (
	screenHome appScreen = iota
	screenZones
	screenRecords
	screenForm
	screenConfirmDelete
	screenDomains
	screenRegisterForm
	screenCertificates
	screenCertificateForm
	screenBilling
	screenDomainSettings
	screenForwardForm
	screenDelegationForm
	screenConfirmAction
)

var homeEntries = []string{"Zones", "Domains", "Billing"}

type browserState struct {
	screen            appScreen
	loading           bool
	status            string
	homeCursor        int
	zoneList          []zones.Zone
	zoneCursor        int
	currentZone       string
	records           []zones.Record
	recordCursor      int
	form              recordForm
	deleteTarget      *zones.Record
	domainList        []domains.Domain
	domainCursor      int
	registerInput     textinput.Model
	currentDomain     string
	certificateList   []certificates.Certificate
	certificateCursor int
	certForm          certificateForm
	chargeList        []billing.Charge

	// domain settings
	dnssecEnabled   bool
	whoisEnabled    bool
	lockEnabled     bool
	delegationList  []string
	delegationInput textinput.Model
	forwardList     []domains.EmailForward
	forwardCursor   int
	forwardForm     [2]textinput.Model // alias, destination
	forwardFocused  int

	// generic yes/no confirmation
	confirmPrompt  string
	confirmCommand tea.Cmd
	confirmReturn  appScreen
}

// The certificate request form: common-name subdomain, alternate names, and
// the renewal mode.
const (
	certFieldName = iota
	certFieldAlternates
	certFieldAutoRenew
	certFieldCount
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

func (form *certificateForm) focus(field int) {
	form.focused = ((field % certFieldCount) + certFieldCount) % certFieldCount
	for index := range form.inputs {
		if index == form.focused {
			form.inputs[index].Prompt = "> "
			form.inputs[index].Focus()
		} else {
			form.inputs[index].Prompt = "  "
			form.inputs[index].Blur()
		}
	}
}

func (form certificateForm) request() (certificates.IssueRequest, error) {
	autoRenewAnswer := strings.ToLower(strings.TrimSpace(form.inputs[certFieldAutoRenew].Value()))
	if autoRenewAnswer != "yes" && autoRenewAnswer != "no" && autoRenewAnswer != "" {
		return certificates.IssueRequest{}, errors.New("auto-renew must be yes or no")
	}
	var alternates []string
	for _, name := range strings.Split(form.inputs[certFieldAlternates].Value(), ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			alternates = append(alternates, trimmed)
		}
	}
	return certificates.IssueRequest{
		Name:           strings.TrimSpace(form.inputs[certFieldName].Value()),
		AlternateNames: alternates,
		AutoRenew:      autoRenewAnswer != "no",
	}, nil
}

func newBrowserState() browserState {
	return browserState{screen: screenHome}
}

// --- messages and commands ------------------------------------------------

type zonesLoadedMessage struct {
	zones []zones.Zone
	err   error
}

type recordsLoadedMessage struct {
	zone    string
	records []zones.Record
	err     error
}

type mutationDoneMessage struct {
	verb string
	err  error
}

type domainsLoadedMessage struct {
	domains []domains.Domain
	err     error
}

type registrationSubmittedMessage struct {
	domainName string
	err        error
}

type certificatesLoadedMessage struct {
	domainName   string
	certificates []certificates.Certificate
	err          error
}

type certificateRequestedMessage struct {
	err error
}

type chargesLoadedMessage struct {
	charges []billing.Charge
	err     error
}

type settingsLoadedMessage struct {
	domainName string
	dnssec     bool
	whois      bool
	lock       bool
	delegation []string
	forwards   []domains.EmailForward
	err        error
}

type settingChangedMessage struct {
	what string
	err  error
}

func (model sessionModel) loadZones() tea.Cmd {
	component, session, ctx := model.components.Zones, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.Zones(ctx, session)
		if outcome.Failed() {
			return zonesLoadedMessage{err: outcome.Error()}
		}
		listed := outcome.Value()
		sort.Slice(listed, func(left, right int) bool { return listed[left].Name < listed[right].Name })
		return zonesLoadedMessage{zones: listed}
	}
}

func (model sessionModel) loadRecords(zoneName string) tea.Cmd {
	component, session, ctx := model.components.Zones, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.Records(ctx, session, zoneName)
		if outcome.Failed() {
			return recordsLoadedMessage{zone: zoneName, err: outcome.Error()}
		}
		listed := outcome.Value()
		sort.Slice(listed, func(left, right int) bool {
			if listed[left].Name != listed[right].Name {
				return listed[left].Name < listed[right].Name
			}
			return listed[left].ID < listed[right].ID
		})
		return recordsLoadedMessage{zone: zoneName, records: listed}
	}
}

func (model sessionModel) submitForm() tea.Cmd {
	component, session, ctx := model.components.Zones, model.session, model.appCtx
	zoneName := model.browser.currentZone
	draft, err := model.browser.form.draft()
	if err != nil {
		return func() tea.Msg { return mutationDoneMessage{verb: "save", err: err} }
	}
	editingID := model.browser.form.editingID
	return func() tea.Msg {
		if editingID != 0 {
			outcome := component.UpdateRecord(ctx, session, zoneName, editingID, draft)
			return mutationDoneMessage{verb: "update", err: outcome.Error()}
		}
		outcome := component.CreateRecord(ctx, session, zoneName, draft)
		return mutationDoneMessage{verb: "create", err: outcome.Error()}
	}
}

func (model sessionModel) deleteRecord(record zones.Record) tea.Cmd {
	component, session, ctx := model.components.Zones, model.session, model.appCtx
	zoneName := model.browser.currentZone
	return func() tea.Msg {
		outcome := component.DeleteRecord(ctx, session, zoneName, record.ID)
		return mutationDoneMessage{verb: "delete", err: outcome.Error()}
	}
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

func (model sessionModel) submitRegistration() tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := strings.ToLower(strings.TrimSpace(model.browser.registerInput.Value()))
	return func() tea.Msg {
		outcome := component.RegisterDomain(ctx, session, domainName)
		return registrationSubmittedMessage{domainName: domainName, err: outcome.Error()}
	}
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

func (model sessionModel) loadSettings(domainName string) tea.Cmd {
	domainsComponent, registrarComponent := model.components.Domains, model.components.Registrar
	session, ctx := model.session, model.appCtx
	return func() tea.Msg {
		dnssec := domainsComponent.DNSSEC(ctx, session, domainName)
		if dnssec.Failed() {
			return settingsLoadedMessage{domainName: domainName, err: dnssec.Error()}
		}
		whois := registrarComponent.WhoisPrivacy(ctx, session, domainName)
		if whois.Failed() {
			return settingsLoadedMessage{domainName: domainName, err: whois.Error()}
		}
		lock := registrarComponent.TransferLock(ctx, session, domainName)
		if lock.Failed() {
			return settingsLoadedMessage{domainName: domainName, err: lock.Error()}
		}
		delegation := registrarComponent.Delegation(ctx, session, domainName)
		if delegation.Failed() {
			return settingsLoadedMessage{domainName: domainName, err: delegation.Error()}
		}
		forwards := domainsComponent.EmailForwards(ctx, session, domainName)
		if forwards.Failed() {
			return settingsLoadedMessage{domainName: domainName, err: forwards.Error()}
		}
		return settingsLoadedMessage{
			domainName: domainName,
			dnssec:     dnssec.Value().Enabled,
			whois:      whois.Value(),
			lock:       lock.Value(),
			delegation: delegation.Value(),
			forwards:   forwards.Value(),
		}
	}
}

func (model sessionModel) setWhoisPrivacy(enabled bool) tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.SetWhoisPrivacy(ctx, session, domainName, enabled)
		return settingChangedMessage{what: "WHOIS privacy", err: outcome.Error()}
	}
}

func (model sessionModel) setTransferLock(enabled bool) tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.SetTransferLock(ctx, session, domainName, enabled)
		return settingChangedMessage{what: "transfer lock", err: outcome.Error()}
	}
}

func (model sessionModel) submitDelegation() tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := model.browser.currentDomain
	var nameServers []string
	for _, nameServer := range strings.Split(model.browser.delegationInput.Value(), ",") {
		if trimmed := strings.ToLower(strings.TrimSpace(nameServer)); trimmed != "" {
			nameServers = append(nameServers, trimmed)
		}
	}
	return func() tea.Msg {
		outcome := component.SetDelegation(ctx, session, domainName, nameServers)
		return settingChangedMessage{what: "delegation", err: outcome.Error()}
	}
}

func (model sessionModel) renewDomain() tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.RenewDomain(ctx, session, domainName)
		return settingChangedMessage{what: "renewal", err: outcome.Error()}
	}
}

func (model sessionModel) setDNSSEC(enabled bool) tea.Cmd {
	component, session, ctx := model.components.Domains, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.SetDNSSEC(ctx, session, domainName, enabled)
		return settingChangedMessage{what: "DNSSEC", err: outcome.Error()}
	}
}

func (model sessionModel) setAutoRenew(enabled bool) tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.SetAutoRenewal(ctx, session, domainName, enabled)
		return settingChangedMessage{what: "auto-renew", err: outcome.Error()}
	}
}

func (model sessionModel) createForward() tea.Cmd {
	component, session, ctx := model.components.Domains, model.session, model.appCtx
	domainName := model.browser.currentDomain
	alias := model.browser.forwardForm[0].Value()
	destination := model.browser.forwardForm[1].Value()
	return func() tea.Msg {
		outcome := component.CreateEmailForward(ctx, session, domainName, alias, destination)
		return settingChangedMessage{what: "forward", err: outcome.Error()}
	}
}

func (model sessionModel) deleteForward(forward domains.EmailForward) tea.Cmd {
	component, session, ctx := model.components.Domains, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.DeleteEmailForward(ctx, session, domainName, forward.ID)
		return settingChangedMessage{what: "forward", err: outcome.Error()}
	}
}

// --- update ----------------------------------------------------------------

func (model sessionModel) updateBrowser(message tea.Msg) (tea.Model, tea.Cmd) {
	browser := &model.browser

	switch message := message.(type) {
	case zonesLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			return model, nil
		}
		browser.zoneList = message.zones
		browser.zoneCursor = clamp(browser.zoneCursor, len(browser.zoneList))
		browser.status = ""
		return model, nil

	case recordsLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenZones
			return model, nil
		}
		browser.currentZone = message.zone
		browser.records = message.records
		browser.recordCursor = clamp(browser.recordCursor, len(browser.records))
		browser.screen = screenRecords
		return model, nil

	case mutationDoneMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			// Validation problems keep the form open for correction;
			// upstream problems drop back to the list.
			var invalid zones.InvalidRecordError
			if message.verb != "delete" && errors.As(message.err, &invalid) {
				browser.screen = screenForm
				return model, nil
			}
			browser.screen = screenRecords
			return model, nil
		}
		browser.status = "✓ " + message.verb + " done"
		browser.screen = screenRecords
		browser.deleteTarget = nil
		browser.loading = true
		return model, tea.Batch(model.loadRecords(browser.currentZone), spinnerTick())

	case domainsLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenHome
			return model, nil
		}
		browser.domainList = message.domains
		browser.domainCursor = clamp(browser.domainCursor, len(browser.domainList))
		browser.screen = screenDomains
		return model, nil

	case registrationSubmittedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			var invalid registrar.InvalidDomainError
			if errors.As(message.err, &invalid) {
				browser.screen = screenRegisterForm
				return model, nil
			}
			browser.screen = screenDomains
			return model, nil
		}
		browser.status = "✓ " + message.domainName + " accepted - registration polls in the background"
		browser.screen = screenDomains
		browser.loading = true
		return model, tea.Batch(model.loadDomains(), spinnerTick())

	case certificatesLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenDomains
			return model, nil
		}
		browser.currentDomain = message.domainName
		browser.certificateList = message.certificates
		browser.certificateCursor = clamp(browser.certificateCursor, len(browser.certificateList))
		browser.screen = screenCertificates
		return model, nil

	case certificateRequestedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenCertificateForm
			return model, nil
		}
		browser.status = "✓ certificate requested - issuance polls in the background"
		browser.screen = screenCertificates
		browser.loading = true
		return model, tea.Batch(model.loadCertificates(browser.currentDomain), spinnerTick())

	case chargesLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenHome
			return model, nil
		}
		browser.chargeList = message.charges
		browser.screen = screenBilling
		return model, nil

	case settingsLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenDomains
			return model, nil
		}
		browser.currentDomain = message.domainName
		browser.dnssecEnabled = message.dnssec
		browser.whoisEnabled = message.whois
		browser.lockEnabled = message.lock
		browser.delegationList = message.delegation
		browser.forwardList = message.forwards
		browser.forwardCursor = clamp(browser.forwardCursor, len(browser.forwardList))
		browser.screen = screenDomainSettings
		return model, nil

	case settingChangedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenDomainSettings
			return model, nil
		}
		browser.status = "✓ " + message.what + " updated"
		browser.loading = true
		return model, tea.Batch(model.loadSettings(browser.currentDomain), spinnerTick())

	case tea.KeyMsg:
		switch browser.screen {
		case screenHome:
			return model.homeKeys(message)
		case screenZones:
			return model.zonesKeys(message)
		case screenRecords:
			return model.recordsKeys(message)
		case screenForm:
			return model.formKeys(message)
		case screenConfirmDelete:
			return model.confirmKeys(message)
		case screenDomains:
			return model.domainsKeys(message)
		case screenRegisterForm:
			return model.registerFormKeys(message)
		case screenCertificates:
			return model.certificatesKeys(message)
		case screenCertificateForm:
			return model.certificateFormKeys(message)
		case screenBilling:
			return model.billingKeys(message)
		case screenDomainSettings:
			return model.settingsKeys(message)
		case screenForwardForm:
			return model.forwardFormKeys(message)
		case screenDelegationForm:
			return model.delegationFormKeys(message)
		case screenConfirmAction:
			return model.confirmActionKeys(message)
		}
	}
	return model, nil
}

func (model sessionModel) settingsKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenDomains
		browser.status = ""
	case "up", "k":
		browser.forwardCursor = clamp(browser.forwardCursor-1, len(browser.forwardList))
	case "down", "j":
		browser.forwardCursor = clamp(browser.forwardCursor+1, len(browser.forwardList))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadSettings(browser.currentDomain), spinnerTick())
	case "x":
		if browser.dnssecEnabled {
			// Disabling DNSSEC on a signed zone can break resolution for
			// validating resolvers until DS records age out - confirm it.
			browser.confirmPrompt = "Disable DNSSEC for " + browser.currentDomain + "?"
			browser.confirmCommand = model.setDNSSEC(false)
			browser.confirmReturn = screenDomainSettings
			browser.screen = screenConfirmAction
			return model, nil
		}
		browser.loading = true
		return model, tea.Batch(model.setDNSSEC(true), spinnerTick())
	case "a":
		enable := true
		for _, domain := range browser.domainList {
			if domain.Name == browser.currentDomain {
				enable = !domain.AutoRenew
			}
		}
		browser.loading = true
		return model, tea.Batch(model.setAutoRenew(enable), spinnerTick())
	case "w":
		browser.loading = true
		return model, tea.Batch(model.setWhoisPrivacy(!browser.whoisEnabled), spinnerTick())
	case "l":
		if browser.lockEnabled {
			// Unlocking is the precondition for a transfer away; make it a
			// deliberate act.
			browser.confirmPrompt = "Disable the transfer lock for " + browser.currentDomain + "?"
			browser.confirmCommand = model.setTransferLock(false)
			browser.confirmReturn = screenDomainSettings
			browser.screen = screenConfirmAction
			return model, nil
		}
		browser.loading = true
		return model, tea.Batch(model.setTransferLock(true), spinnerTick())
	case "e":
		input := textinput.New()
		input.CharLimit = 500
		input.Width = 60
		input.Prompt = "> "
		input.PromptStyle = model.styles.selected
		input.SetValue(strings.Join(browser.delegationList, ", "))
		input.Focus()
		browser.delegationInput = input
		browser.screen = screenDelegationForm
		browser.status = ""
		return model, textinput.Blink
	case "R":
		browser.confirmPrompt = "Renew " + browser.currentDomain + " for 1 year? This charges your account."
		browser.confirmCommand = model.renewDomain()
		browser.confirmReturn = screenDomainSettings
		browser.screen = screenConfirmAction
		return model, nil
	case "n":
		for field := range 2 {
			input := textinput.New()
			input.CharLimit = 253
			input.Width = 40
			input.Prompt = "  "
			input.PromptStyle = model.styles.selected
			browser.forwardForm[field] = input
		}
		browser.forwardForm[0].Prompt = "> "
		browser.forwardForm[0].Placeholder = "alias (the part before @" + browser.currentDomain + ")"
		browser.forwardForm[1].Placeholder = "destination email"
		browser.forwardForm[0].Focus()
		browser.forwardFocused = 0
		browser.screen = screenForwardForm
		browser.status = ""
		return model, textinput.Blink
	case "d":
		if len(browser.forwardList) == 0 {
			return model, nil
		}
		forward := browser.forwardList[browser.forwardCursor]
		browser.confirmPrompt = "Delete the forward " + forward.From + " -> " + forward.To + "?"
		browser.confirmCommand = model.deleteForward(forward)
		browser.confirmReturn = screenDomainSettings
		browser.screen = screenConfirmAction
	}
	return model, nil
}

func (model sessionModel) forwardFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenDomainSettings
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.createForward(), spinnerTick())
	case "tab", "down", "shift+tab", "up":
		browser.forwardFocused = (browser.forwardFocused + 1) % 2
		for field := range 2 {
			if field == browser.forwardFocused {
				browser.forwardForm[field].Prompt = "> "
				browser.forwardForm[field].Focus()
			} else {
				browser.forwardForm[field].Prompt = "  "
				browser.forwardForm[field].Blur()
			}
		}
		return model, nil
	}
	var inputCommand tea.Cmd
	browser.forwardForm[browser.forwardFocused], inputCommand =
		browser.forwardForm[browser.forwardFocused].Update(key)
	return model, inputCommand
}

func (model sessionModel) delegationFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenDomainSettings
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.submitDelegation(), spinnerTick())
	}
	var inputCommand tea.Cmd
	browser.delegationInput, inputCommand = browser.delegationInput.Update(key)
	return model, inputCommand
}

func (model sessionModel) confirmActionKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "y":
		command := browser.confirmCommand
		browser.confirmCommand = nil
		browser.screen = browser.confirmReturn
		browser.loading = true
		return model, tea.Batch(command, spinnerTick())
	case "n", "esc":
		browser.confirmCommand = nil
		browser.screen = browser.confirmReturn
	}
	return model, nil
}

func (model sessionModel) certificatesKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenDomains
		browser.status = ""
	case "up", "k":
		browser.certificateCursor = clamp(browser.certificateCursor-1, len(browser.certificateList))
	case "down", "j":
		browser.certificateCursor = clamp(browser.certificateCursor+1, len(browser.certificateList))
	case "pgup":
		browser.certificateCursor = clamp(browser.certificateCursor-model.visibleRows(), len(browser.certificateList))
	case "pgdown":
		browser.certificateCursor = clamp(browser.certificateCursor+model.visibleRows(), len(browser.certificateList))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadCertificates(browser.currentDomain), spinnerTick())
	case "i":
		browser.certForm = newCertificateForm(model.styles)
		browser.screen = screenCertificateForm
		browser.status = ""
		return model, textinput.Blink
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

func (model sessionModel) homeKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q", "esc":
		return model, tea.Quit
	case "up", "k":
		browser.homeCursor = clamp(browser.homeCursor-1, len(homeEntries))
	case "down", "j":
		browser.homeCursor = clamp(browser.homeCursor+1, len(homeEntries))
	case "enter":
		browser.loading = true
		browser.status = ""
		switch homeEntries[browser.homeCursor] {
		case "Zones":
			browser.screen = screenZones
			return model, tea.Batch(model.loadZones(), spinnerTick())
		case "Domains":
			browser.screen = screenDomains
			return model, tea.Batch(model.loadDomains(), spinnerTick())
		default:
			browser.screen = screenBilling
			return model, tea.Batch(model.loadCharges(), spinnerTick())
		}
	}
	return model, nil
}

func (model sessionModel) domainsKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.domainCursor = clamp(browser.domainCursor-1, len(browser.domainList))
	case "down", "j":
		browser.domainCursor = clamp(browser.domainCursor+1, len(browser.domainList))
	case "pgup":
		browser.domainCursor = clamp(browser.domainCursor-model.visibleRows(), len(browser.domainList))
	case "pgdown":
		browser.domainCursor = clamp(browser.domainCursor+model.visibleRows(), len(browser.domainList))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadDomains(), spinnerTick())
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
		if len(browser.domainList) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		browser.certificateCursor = 0
		return model, tea.Batch(
			model.loadCertificates(browser.domainList[browser.domainCursor].Name), spinnerTick())
	case "s":
		if len(browser.domainList) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		browser.forwardCursor = 0
		return model, tea.Batch(
			model.loadSettings(browser.domainList[browser.domainCursor].Name), spinnerTick())
	case "c":
		// Straight to the certificate form for the selected domain.
		if len(browser.domainList) == 0 {
			return model, nil
		}
		browser.currentDomain = browser.domainList[browser.domainCursor].Name
		browser.certForm = newCertificateForm(model.styles)
		browser.screen = screenCertificateForm
		browser.status = ""
		return model, textinput.Blink
	}
	return model, nil
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
		return model, tea.Batch(model.submitRegistration(), spinnerTick())
	}
	var inputCommand tea.Cmd
	browser.registerInput, inputCommand = browser.registerInput.Update(key)
	return model, inputCommand
}

func (model sessionModel) zonesKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.zoneCursor = clamp(browser.zoneCursor-1, len(browser.zoneList))
	case "down", "j":
		browser.zoneCursor = clamp(browser.zoneCursor+1, len(browser.zoneList))
	case "pgup":
		browser.zoneCursor = clamp(browser.zoneCursor-model.visibleRows(), len(browser.zoneList))
	case "pgdown":
		browser.zoneCursor = clamp(browser.zoneCursor+model.visibleRows(), len(browser.zoneList))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadZones(), spinnerTick())
	case "enter":
		if len(browser.zoneList) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		browser.recordCursor = 0
		return model, tea.Batch(model.loadRecords(browser.zoneList[browser.zoneCursor].Name), spinnerTick())
	}
	return model, nil
}

func (model sessionModel) recordsKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenZones
		browser.status = ""
	case "up", "k":
		browser.recordCursor = clamp(browser.recordCursor-1, len(browser.records))
	case "down", "j":
		browser.recordCursor = clamp(browser.recordCursor+1, len(browser.records))
	case "pgup":
		browser.recordCursor = clamp(browser.recordCursor-model.visibleRows(), len(browser.records))
	case "pgdown":
		browser.recordCursor = clamp(browser.recordCursor+model.visibleRows(), len(browser.records))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadRecords(browser.currentZone), spinnerTick())
	case "n":
		browser.form = newRecordForm(nil, model.styles)
		browser.screen = screenForm
		browser.status = ""
		return model, textinput.Blink
	case "e":
		if len(browser.records) == 0 {
			return model, nil
		}
		record := browser.records[browser.recordCursor]
		browser.form = newRecordForm(&record, model.styles)
		browser.screen = screenForm
		browser.status = ""
		return model, textinput.Blink
	case "d":
		if len(browser.records) == 0 {
			return model, nil
		}
		record := browser.records[browser.recordCursor]
		browser.deleteTarget = &record
		browser.screen = screenConfirmDelete
	}
	return model, nil
}

func (model sessionModel) formKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenRecords
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.submitForm(), spinnerTick())
	case "tab", "down":
		browser.form.focus(browser.form.focused + 1)
		return model, nil
	case "shift+tab", "up":
		browser.form.focus(browser.form.focused - 1)
		return model, nil
	}
	var inputCommand tea.Cmd
	browser.form.inputs[browser.form.focused], inputCommand =
		browser.form.inputs[browser.form.focused].Update(key)
	return model, inputCommand
}

func (model sessionModel) confirmKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "y":
		if browser.deleteTarget == nil {
			browser.screen = screenRecords
			return model, nil
		}
		browser.loading = true
		return model, tea.Batch(model.deleteRecord(*browser.deleteTarget), spinnerTick())
	case "n", "esc":
		browser.deleteTarget = nil
		browser.screen = screenRecords
	}
	return model, nil
}

// --- the record form -------------------------------------------------------

const (
	fieldName = iota
	fieldType
	fieldContent
	fieldTTL
	fieldPriority
	fieldCount
)

type recordForm struct {
	inputs    [fieldCount]textinput.Model
	focused   int
	editingID int64 // 0 means create
}

func newRecordForm(existing *zones.Record, styles styleSet) recordForm {
	form := recordForm{}
	labels := [fieldCount]string{"name (empty = apex)", "type (A, CNAME, MX, ...)", "content", "ttl", "priority"}
	for field := range fieldCount {
		input := textinput.New()
		input.Placeholder = labels[field]
		input.CharLimit = 253
		input.Width = 40
		input.Prompt = "  "
		input.PromptStyle = styles.selected
		form.inputs[field] = input
	}
	form.inputs[fieldTTL].SetValue("3600")
	if existing != nil {
		form.editingID = existing.ID
		form.inputs[fieldName].SetValue(existing.Name)
		form.inputs[fieldType].SetValue(existing.Type)
		form.inputs[fieldContent].SetValue(existing.Content)
		form.inputs[fieldTTL].SetValue(strconv.Itoa(existing.TTL))
		if existing.Priority > 0 {
			form.inputs[fieldPriority].SetValue(strconv.Itoa(existing.Priority))
		}
	}
	form.focus(0)
	return form
}

func (form *recordForm) focus(field int) {
	form.focused = ((field % fieldCount) + fieldCount) % fieldCount
	for index := range form.inputs {
		if index == form.focused {
			form.inputs[index].Prompt = "> "
			form.inputs[index].Focus()
		} else {
			form.inputs[index].Prompt = "  "
			form.inputs[index].Blur()
		}
	}
}

func (form recordForm) draft() (zones.RecordDraft, error) {
	ttl, err := strconv.Atoi(strings.TrimSpace(form.inputs[fieldTTL].Value()))
	if err != nil {
		return zones.RecordDraft{}, zones.InvalidRecordError{Reason: "TTL must be a number"}
	}
	priority := 0
	if raw := strings.TrimSpace(form.inputs[fieldPriority].Value()); raw != "" {
		priority, err = strconv.Atoi(raw)
		if err != nil {
			return zones.RecordDraft{}, zones.InvalidRecordError{Reason: "priority must be a number"}
		}
	}
	return zones.RecordDraft{
		Name:     strings.TrimSpace(form.inputs[fieldName].Value()),
		Type:     strings.ToUpper(strings.TrimSpace(form.inputs[fieldType].Value())),
		Content:  strings.TrimSpace(form.inputs[fieldContent].Value()),
		TTL:      ttl,
		Priority: priority,
	}, nil
}

// --- views -----------------------------------------------------------------

func (model sessionModel) viewBrowser() string {
	browser := model.browser
	var body string
	switch browser.screen {
	case screenHome:
		body = model.viewHome()
	case screenZones:
		body = model.viewZones()
	case screenRecords:
		body = model.viewRecords()
	case screenForm:
		body = model.viewForm()
	case screenConfirmDelete:
		body = model.viewConfirm()
	case screenDomains:
		body = model.viewDomains()
	case screenRegisterForm:
		body = model.viewRegisterForm()
	case screenCertificates:
		body = model.viewCertificates()
	case screenCertificateForm:
		body = model.viewCertificateForm()
	case screenBilling:
		body = model.viewBilling()
	case screenDomainSettings:
		body = model.viewSettings()
	case screenForwardForm:
		body = model.viewForwardForm()
	case screenDelegationForm:
		body = model.viewDelegationForm()
	case screenConfirmAction:
		body = model.viewConfirmAction()
	}
	return model.styles.frame.Render(body) + "\n"
}

func (model sessionModel) viewSettings() string {
	browser := model.browser
	dnssec := "disabled"
	if browser.dnssecEnabled {
		dnssec = "enabled"
	}
	autoRenew := "manual"
	for _, domain := range browser.domainList {
		if domain.Name == browser.currentDomain && domain.AutoRenew {
			autoRenew = "auto-renew"
		}
	}
	whois := "disabled"
	if browser.whoisEnabled {
		whois = "enabled"
	}
	lock := "disabled"
	if browser.lockEnabled {
		lock = "enabled"
	}
	lines := []string{
		model.header(browser.currentDomain + " · settings"),
		"",
		"  DNSSEC          " + dnssec,
		"  Renewal         " + autoRenew,
		"  WHOIS privacy   " + whois,
		"  Transfer lock   " + lock,
		"  Delegation      " + strings.Join(browser.delegationList, ", "),
		"",
		model.styles.subtle.Render("  email forwards"),
	}
	if len(browser.forwardList) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("    none; n creates one"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.forwardCursor, len(browser.forwardList), visible)
	for index := start; index < end; index++ {
		forward := browser.forwardList[index]
		line := fmt.Sprintf("%-28.28s -> %s", forward.From, forward.To)
		cursor := "    "
		if index == browser.forwardCursor {
			cursor = "  > "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("x dnssec · a renewal · w whois · l lock · e delegation · R renew · n/d forwards · esc back"))...)
}

func (model sessionModel) viewDelegationForm() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		model.header(model.browser.currentDomain+" · delegation"),
		"",
		"  Name servers (comma-separated, 2-6)",
		"  "+model.browser.delegationInput.View(),
		model.footer("enter save · esc cancel"),
	)
}

func (model sessionModel) viewForwardForm() string {
	browser := model.browser
	return lipgloss.JoinVertical(lipgloss.Left,
		model.header(browser.currentDomain+" · new email forward"),
		"",
		"  Alias        "+browser.forwardForm[0].View(),
		"  Destination  "+browser.forwardForm[1].View(),
		model.footer("tab next field · enter create · esc cancel"),
	)
}

func (model sessionModel) viewConfirmAction() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		model.header(model.browser.currentDomain),
		"",
		model.styles.errorText.Render(model.browser.confirmPrompt),
		model.footer("y confirm · n cancel"),
	)
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
	if len(browser.certificateList) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no certificates; i issues a Let's Encrypt one"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.certificateCursor, len(browser.certificateList), visible)
	for index := start; index < end; index++ {
		certificate := browser.certificateList[index]
		line := fmt.Sprintf("%-30.30s %-12s %-10s expires %s",
			certificate.CommonName, certificate.State,
			autoRenewLabel(certificate.AutoRenew), certificate.ExpiresAt)
		cursor := "  "
		if index == browser.certificateCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.certificateCursor, len(browser.certificateList), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("i issue · r refresh · esc back · q quit"))...)
}

func (model sessionModel) viewBilling() string {
	browser := model.browser
	lines := []string{model.header("billing"), ""}
	if len(browser.chargeList) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no charges on this account"))
	}
	for _, charge := range browser.chargeList {
		invoiced := charge.InvoicedAt
		if len(invoiced) >= 10 {
			invoiced = invoiced[:10]
		}
		lines = append(lines, fmt.Sprintf("  %s  %-12s %10s  %s",
			invoiced, charge.State, charge.TotalAmount, charge.Reference))
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("r refresh · esc back · q quit"))...)
}

func (model sessionModel) viewHome() string {
	lines := []string{model.header("home"), ""}
	for index, entry := range homeEntries {
		cursor := "  "
		if index == model.browser.homeCursor {
			cursor = "> "
			entry = model.styles.selected.Render(entry)
		}
		lines = append(lines, cursor+entry)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("enter open · q quit"))...)
}

func (model sessionModel) viewDomains() string {
	browser := model.browser
	lines := []string{model.header("domains"), ""}
	if len(browser.domainList) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no domains; n registers one"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.domainCursor, len(browser.domainList), visible)
	for index := start; index < end; index++ {
		domain := browser.domainList[index]
		line := fmt.Sprintf("%-30.30s %-12s %-10s renews %s",
			domain.Name, domain.State, autoRenewLabel(domain.AutoRenew), domain.ExpiresAt)
		cursor := "  "
		if index == browser.domainCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.domainCursor, len(browser.domainList), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("enter certificates · s settings · n register · c certificate · r refresh · esc back · q quit"))...)
}

func autoRenewLabel(autoRenew bool) string {
	if autoRenew {
		return "auto-renew"
	}
	return "manual"
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

func (model sessionModel) header(crumb string) string {
	title := model.styles.title.Render("DNSSHIMPLE")
	account := model.styles.subtle.Render(fmt.Sprintf(" %s · account #%d", crumb, model.session.Tenant.AccountID))
	return title + account
}

func (model sessionModel) footer(hints string) string {
	lines := []string{""}
	if model.browser.loading {
		lines = append(lines, spinnerFrames[model.spinnerFrame]+" talking to DNSimple...")
	}
	if model.browser.status != "" {
		lines = append(lines, model.styles.status.Render(model.browser.status))
	}
	lines = append(lines, model.styles.subtle.Render(hints))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (model sessionModel) viewZones() string {
	browser := model.browser
	lines := []string{model.header("zones"), ""}
	if len(browser.zoneList) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no zones in this account"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.zoneCursor, len(browser.zoneList), visible)
	for index := start; index < end; index++ {
		cursor := "  "
		line := browser.zoneList[index].Name
		if index == browser.zoneCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.zoneCursor, len(browser.zoneList), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("enter open · r refresh · q quit"))...)
}

func (model sessionModel) viewRecords() string {
	browser := model.browser
	lines := []string{model.header(browser.currentZone), ""}
	if len(browser.records) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no records; n creates the first one"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.recordCursor, len(browser.records), visible)
	for index := start; index < end; index++ {
		record := browser.records[index]
		name := record.Name
		if name == "" {
			name = "@"
		}
		line := fmt.Sprintf("%-20.20s %-6s %-32.32s %6d", name, record.Type, record.Content, record.TTL)
		cursor := "  "
		if index == browser.recordCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.recordCursor, len(browser.records), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("n new · e edit · d delete · r refresh · esc back · q quit"))...)
}

func (model sessionModel) viewForm() string {
	browser := model.browser
	verb := "new record"
	if browser.form.editingID != 0 {
		verb = fmt.Sprintf("edit record #%d", browser.form.editingID)
	}
	lines := []string{model.header(browser.currentZone + " · " + verb), ""}
	labels := [fieldCount]string{"Name    ", "Type    ", "Content ", "TTL     ", "Priority"}
	for field := range fieldCount {
		lines = append(lines, "  "+labels[field]+" "+browser.form.inputs[field].View())
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("tab next field · enter save · esc cancel"))...)
}

func (model sessionModel) viewConfirm() string {
	browser := model.browser
	target := browser.deleteTarget
	description := ""
	if target != nil {
		name := target.Name
		if name == "" {
			name = "@"
		}
		description = fmt.Sprintf("%s %s %s (ttl %d)", name, target.Type, target.Content, target.TTL)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		model.header(browser.currentZone),
		"",
		model.styles.errorText.Render("Delete this record?"),
		"",
		"  "+description,
		model.footer("y delete · n cancel"),
	)
}

// --- helpers ---------------------------------------------------------------

// statusFor turns failures into next actions: what went wrong in DNSimple's
// own words when it has them, and what to do about it.
func statusFor(err error) string {
	if rateLimited, isRateLimited := gateway.IsRateLimited(err); isRateLimited {
		return "⚠ DNSimple rate limit hit - resuming at " + rateLimited.ResumeAt.Local().Format("15:04")
	}
	if errors.Is(err, gateway.ErrUpstreamDown) {
		return "⚠ DNSimple is struggling - wait a minute, then retry (r)"
	}
	var rejection gateway.UpstreamRejectionError
	if errors.As(err, &rejection) {
		return "✗ DNSimple says: " + rejection.Message
	}
	var timedOut gateway.UpstreamTimeoutError
	if errors.As(err, &timedOut) {
		return "⚠ DNSimple is slow - it may still have completed; refresh (r) in a minute before retrying"
	}
	var invalidRecord zones.InvalidRecordError
	if errors.As(err, &invalidRecord) {
		return "✗ " + invalidRecord.Reason
	}
	var invalidDomain registrar.InvalidDomainError
	if errors.As(err, &invalidDomain) {
		return "✗ " + invalidDomain.Reason
	}
	if errors.Is(err, registrar.ErrDomainTaken) {
		return "✗ that domain is taken - try another name"
	}
	if errors.Is(err, registrar.ErrNoContacts) {
		return "✗ your account has no contacts - create one at dnsimple.com, then retry"
	}
	message := err.Error()
	if len(message) > 100 {
		message = message[:100] + "..."
	}
	return "✗ " + message
}

// visibleRows is how many list rows fit between the header and the footer.
func (model sessionModel) visibleRows() int {
	const chrome = 12 // border, padding, header, footer, status
	if model.height <= 0 {
		return 10
	}
	if model.height-chrome < 4 {
		return 4
	}
	return model.height - chrome
}

// listWindow slides a visible-sized window along the list, keeping the
// cursor near the middle.
func listWindow(cursor, total, visible int) (int, int) {
	if total <= visible {
		return 0, total
	}
	start := cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}

func (model sessionModel) positionLine(cursor, total, visible int) string {
	if total <= visible {
		return ""
	}
	return model.styles.subtle.Render(fmt.Sprintf("  %d/%d", cursor+1, total))
}

func clamp(cursor, length int) int {
	if length == 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= length {
		return length - 1
	}
	return cursor
}
