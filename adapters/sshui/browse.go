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
	"dnsshimple/core/contacts"
	"dnsshimple/core/domains"
	"dnsshimple/core/gateway"
	"dnsshimple/core/pushes"
	"dnsshimple/core/registrar"
	"dnsshimple/core/services"
	"dnsshimple/core/templates"
	"dnsshimple/core/webhooks"
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
	screenTransferForm
	screenContacts
	screenContactForm
	screenTemplates
	screenTemplateForm
	screenTemplateRecords
	screenTemplatePicker
	screenDomainServices
	screenServicePicker
	screenPushes
	screenPushForm
	screenAllCertificates
	screenCertDomainPicker
	screenRegistrantPicker
	screenWebhooks
	screenWebhookForm
	screenConfirmAction
)

var homeEntries = []string{"Zones", "Domains", "Certificates", "Templates", "Contacts", "Pushes", "Webhooks", "Billing"}

type browserState struct {
	screen          appScreen
	loading         bool
	status          string
	homeCursor      int
	zoneList        []zones.Zone
	zoneCursor      int
	currentZone     string
	records         []zones.Record
	recordCursor    int
	form            recordForm
	deleteTarget    *zones.Record
	webhookList     []webhooks.Webhook
	webhookCursor   int
	webhookURLInput textinput.Model

	allCertList       []certificateOverview
	allCertCursor     int
	certDomainCursor  int
	registrantCursor  int
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

	transferForm    [2]textinput.Model // domain, auth code
	transferFocused int

	templateList         []templates.Template
	templateCursor       int
	currentTemplate      templates.Template
	templateRecordList   []zones.Record
	templateRecordCursor int
	templateForm         [3]textinput.Model // name, sid, description
	templateFocused      int
	formForTemplate      bool

	serviceList         []services.Service
	serviceCursor       int
	availableServices   []services.Service
	servicePickerCursor int
	pushList            []pushes.Push
	pushCursor          int
	pushEmailInput      textinput.Model

	contactList      []contacts.Contact
	contactCursor    int
	contactForm      [contactFieldCount]textinput.Model
	contactFocused   int
	editingContactID int64

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

type transferSubmittedMessage struct {
	domainName string
	err        error
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

func (model sessionModel) submitTransfer() tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := strings.ToLower(strings.TrimSpace(model.browser.transferForm[0].Value()))
	authCode := model.browser.transferForm[1].Value()
	return func() tea.Msg {
		outcome := component.TransferDomain(ctx, session, domainName, authCode)
		return transferSubmittedMessage{domainName: domainName, err: outcome.Error()}
	}
}

func (model sessionModel) authorizeTransferOut() tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.AuthorizeTransferOut(ctx, session, domainName)
		return settingChangedMessage{what: "transfer-out authorization (auth code sent by email)", err: outcome.Error()}
	}
}

func (model sessionModel) restoreDomain() tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.RestoreDomain(ctx, session, domainName)
		return settingChangedMessage{what: "restore order", err: outcome.Error()}
	}
}

const (
	contactFieldFirst = iota
	contactFieldLast
	contactFieldEmail
	contactFieldPhone
	contactFieldAddress
	contactFieldCity
	contactFieldState
	contactFieldPostal
	contactFieldCountry
	contactFieldCount
)

var contactFieldLabels = [contactFieldCount]string{
	"First name", "Last name", "Email", "Phone (+intl)",
	"Address", "City", "State/Province", "Postal code", "Country (2-letter)",
}

type contactsLoadedMessage struct {
	contacts []contacts.Contact
	err      error
}

type contactMutatedMessage struct {
	verb string
	err  error
}

func (model sessionModel) loadContacts() tea.Cmd {
	component, session, ctx := model.components.Contacts, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.Contacts(ctx, session)
		if outcome.Failed() {
			return contactsLoadedMessage{err: outcome.Error()}
		}
		listed := outcome.Value()
		sort.Slice(listed, func(left, right int) bool { return listed[left].ID < listed[right].ID })
		return contactsLoadedMessage{contacts: listed}
	}
}

func (model sessionModel) submitContact() tea.Cmd {
	component, session, ctx := model.components.Contacts, model.session, model.appCtx
	form := model.browser.contactForm
	draft := contacts.Draft{
		FirstName:     strings.TrimSpace(form[contactFieldFirst].Value()),
		LastName:      strings.TrimSpace(form[contactFieldLast].Value()),
		Email:         strings.TrimSpace(form[contactFieldEmail].Value()),
		Phone:         strings.TrimSpace(form[contactFieldPhone].Value()),
		Address:       strings.TrimSpace(form[contactFieldAddress].Value()),
		City:          strings.TrimSpace(form[contactFieldCity].Value()),
		StateProvince: strings.TrimSpace(form[contactFieldState].Value()),
		PostalCode:    strings.TrimSpace(form[contactFieldPostal].Value()),
		Country:       strings.ToUpper(strings.TrimSpace(form[contactFieldCountry].Value())),
	}
	editingID := model.browser.editingContactID
	return func() tea.Msg {
		if editingID != 0 {
			outcome := component.UpdateContact(ctx, session, editingID, draft)
			return contactMutatedMessage{verb: "update", err: outcome.Error()}
		}
		outcome := component.CreateContact(ctx, session, draft)
		return contactMutatedMessage{verb: "create", err: outcome.Error()}
	}
}

func (model sessionModel) deleteContact(contact contacts.Contact) tea.Cmd {
	component, session, ctx := model.components.Contacts, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.DeleteContact(ctx, session, contact.ID)
		return contactMutatedMessage{verb: "delete", err: outcome.Error()}
	}
}

type templatesLoadedMessage struct {
	templates []templates.Template
	picker    bool // true: choosing a template to apply to currentDomain
	err       error
}

type templateMutatedMessage struct {
	verb string
	err  error
}

type templateRecordsLoadedMessage struct {
	template templates.Template
	records  []zones.Record
	err      error
}

type templateRecordMutatedMessage struct {
	verb string
	err  error
}

type templateAppliedMessage struct {
	templateName string
	err          error
}

func (model sessionModel) loadTemplates(picker bool) tea.Cmd {
	component, session, ctx := model.components.Templates, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.Templates(ctx, session)
		if outcome.Failed() {
			return templatesLoadedMessage{picker: picker, err: outcome.Error()}
		}
		listed := outcome.Value()
		sort.Slice(listed, func(left, right int) bool { return listed[left].Name < listed[right].Name })
		return templatesLoadedMessage{templates: listed, picker: picker}
	}
}

func (model sessionModel) submitTemplate() tea.Cmd {
	component, session, ctx := model.components.Templates, model.session, model.appCtx
	draft := templates.Template{
		Name:        model.browser.templateForm[0].Value(),
		SID:         model.browser.templateForm[1].Value(),
		Description: model.browser.templateForm[2].Value(),
	}
	return func() tea.Msg {
		outcome := component.CreateTemplate(ctx, session, draft)
		return templateMutatedMessage{verb: "create", err: outcome.Error()}
	}
}

func (model sessionModel) deleteTemplate(template templates.Template) tea.Cmd {
	component, session, ctx := model.components.Templates, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.DeleteTemplate(ctx, session, template.ID)
		return templateMutatedMessage{verb: "delete", err: outcome.Error()}
	}
}

func (model sessionModel) loadTemplateRecords(template templates.Template) tea.Cmd {
	component, session, ctx := model.components.Templates, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.TemplateRecords(ctx, session, template.ID)
		if outcome.Failed() {
			return templateRecordsLoadedMessage{template: template, err: outcome.Error()}
		}
		listed := outcome.Value()
		sort.Slice(listed, func(left, right int) bool { return listed[left].ID < listed[right].ID })
		return templateRecordsLoadedMessage{template: template, records: listed}
	}
}

func (model sessionModel) submitTemplateRecord() tea.Cmd {
	component, session, ctx := model.components.Templates, model.session, model.appCtx
	templateID := model.browser.currentTemplate.ID
	draft, err := model.browser.form.draft()
	if err != nil {
		return func() tea.Msg { return templateRecordMutatedMessage{verb: "save", err: err} }
	}
	return func() tea.Msg {
		outcome := component.CreateTemplateRecord(ctx, session, templateID, draft)
		return templateRecordMutatedMessage{verb: "create", err: outcome.Error()}
	}
}

func (model sessionModel) deleteTemplateRecord(record zones.Record) tea.Cmd {
	component, session, ctx := model.components.Templates, model.session, model.appCtx
	templateID := model.browser.currentTemplate.ID
	return func() tea.Msg {
		outcome := component.DeleteTemplateRecord(ctx, session, templateID, record.ID)
		return templateRecordMutatedMessage{verb: "delete", err: outcome.Error()}
	}
}

func (model sessionModel) applyTemplate(template templates.Template) tea.Cmd {
	component, session, ctx := model.components.Templates, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.ApplyTemplate(ctx, session, domainName, template.ID)
		return templateAppliedMessage{templateName: template.Name, err: outcome.Error()}
	}
}

type servicesLoadedMessage struct {
	applied   []services.Service
	available []services.Service
	picker    bool
	err       error
}

type serviceChangedMessage struct {
	verb string
	err  error
}

type pushesLoadedMessage struct {
	pushes []pushes.Push
	err    error
}

type pushResolvedMessage struct {
	verb string
	err  error
}

func (model sessionModel) loadServices(picker bool) tea.Cmd {
	component, session, ctx := model.components.Services, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		applied := component.AppliedServices(ctx, session, domainName)
		if applied.Failed() {
			return servicesLoadedMessage{picker: picker, err: applied.Error()}
		}
		if !picker {
			return servicesLoadedMessage{applied: applied.Value()}
		}
		available := component.AvailableServices(ctx, session)
		if available.Failed() {
			return servicesLoadedMessage{picker: true, err: available.Error()}
		}
		// The picker offers only what is not already applied.
		appliedIDs := map[int64]bool{}
		for _, service := range applied.Value() {
			appliedIDs[service.ID] = true
		}
		var offerable []services.Service
		for _, service := range available.Value() {
			if !appliedIDs[service.ID] {
				offerable = append(offerable, service)
			}
		}
		return servicesLoadedMessage{applied: applied.Value(), available: offerable, picker: true}
	}
}

func (model sessionModel) applyService(service services.Service) tea.Cmd {
	component, session, ctx := model.components.Services, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.ApplyService(ctx, session, domainName, service.ID)
		return serviceChangedMessage{verb: "apply", err: outcome.Error()}
	}
}

func (model sessionModel) unapplyService(service services.Service) tea.Cmd {
	component, session, ctx := model.components.Services, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.UnapplyService(ctx, session, domainName, service.ID)
		return serviceChangedMessage{verb: "remove", err: outcome.Error()}
	}
}

func (model sessionModel) loadPushes() tea.Cmd {
	component, session, ctx := model.components.Pushes, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.Pushes(ctx, session)
		if outcome.Failed() {
			return pushesLoadedMessage{err: outcome.Error()}
		}
		return pushesLoadedMessage{pushes: outcome.Value()}
	}
}

func (model sessionModel) initiatePush() tea.Cmd {
	component, session, ctx := model.components.Pushes, model.session, model.appCtx
	domainName := model.browser.currentDomain
	email := strings.TrimSpace(model.browser.pushEmailInput.Value())
	return func() tea.Msg {
		outcome := component.InitiatePush(ctx, session, domainName, email)
		return pushResolvedMessage{verb: "initiate", err: outcome.Error()}
	}
}

// acceptPush resolves the registrant inside the command: accepting needs a
// contact, and the account's first one is the same default registrations use.
func (model sessionModel) acceptPush(push pushes.Push) tea.Cmd {
	pushesComponent, contactsComponent := model.components.Pushes, model.components.Contacts
	session, ctx := model.session, model.appCtx
	return func() tea.Msg {
		listed := contactsComponent.Contacts(ctx, session)
		if listed.Failed() {
			return pushResolvedMessage{verb: "accept", err: listed.Error()}
		}
		if len(listed.Value()) == 0 {
			return pushResolvedMessage{verb: "accept", err: registrar.ErrNoContacts}
		}
		outcome := pushesComponent.AcceptPush(ctx, session, push.ID, listed.Value()[0].ID)
		return pushResolvedMessage{verb: "accept", err: outcome.Error()}
	}
}

func (model sessionModel) rejectPush(push pushes.Push) tea.Cmd {
	component, session, ctx := model.components.Pushes, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.RejectPush(ctx, session, push.ID)
		return pushResolvedMessage{verb: "reject", err: outcome.Error()}
	}
}

// certificateOverview is one row of the account-wide certificate view.
type certificateOverview struct {
	Domain      string
	Certificate certificates.Certificate
}

type allCertificatesLoadedMessage struct {
	items []certificateOverview
	err   error
}

type registrantChangedMessage struct {
	err error
}

// loadAllCertificates aggregates certificates across the portfolio - one
// call per domain, absorbed by the 45 s read cache on refreshes.
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

type registrantContactsLoadedMessage struct {
	contacts []contacts.Contact
	err      error
}

func (model sessionModel) loadContactsForRegistrant() tea.Cmd {
	component, session, ctx := model.components.Contacts, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.Contacts(ctx, session)
		if outcome.Failed() {
			return registrantContactsLoadedMessage{err: outcome.Error()}
		}
		return registrantContactsLoadedMessage{contacts: outcome.Value()}
	}
}

func (model sessionModel) changeRegistrant(contact contacts.Contact) tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	var domainID int64
	for _, domain := range model.browser.domainList {
		if domain.Name == model.browser.currentDomain {
			domainID = domain.ID
		}
	}
	return func() tea.Msg {
		outcome := component.ChangeRegistrant(ctx, session, domainID, contact.ID)
		return registrantChangedMessage{err: outcome.Error()}
	}
}

type webhooksLoadedMessage struct {
	webhooks []webhooks.Webhook
	err      error
}

type webhookMutatedMessage struct {
	verb string
	err  error
}

type zoneActivationMessage struct {
	err error
}

type zoneDistributionMessage struct {
	zoneName    string
	distributed bool
	err         error
}

func (model sessionModel) loadWebhooks() tea.Cmd {
	component, session, ctx := model.components.Webhooks, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.Webhooks(ctx, session)
		if outcome.Failed() {
			return webhooksLoadedMessage{err: outcome.Error()}
		}
		return webhooksLoadedMessage{webhooks: outcome.Value()}
	}
}

func (model sessionModel) createWebhook() tea.Cmd {
	component, session, ctx := model.components.Webhooks, model.session, model.appCtx
	url := strings.TrimSpace(model.browser.webhookURLInput.Value())
	return func() tea.Msg {
		outcome := component.CreateWebhook(ctx, session, url)
		return webhookMutatedMessage{verb: "create", err: outcome.Error()}
	}
}

func (model sessionModel) deleteWebhook(webhook webhooks.Webhook) tea.Cmd {
	component, session, ctx := model.components.Webhooks, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.DeleteWebhook(ctx, session, webhook.ID)
		return webhookMutatedMessage{verb: "delete", err: outcome.Error()}
	}
}

func (model sessionModel) setZoneActivation(zoneName string, active bool) tea.Cmd {
	component, session, ctx := model.components.Zones, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.SetZoneActivation(ctx, session, zoneName, active)
		return zoneActivationMessage{err: outcome.Error()}
	}
}

func (model sessionModel) checkZoneDistribution(zoneName string) tea.Cmd {
	component, session, ctx := model.components.Zones, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.ZoneDistribution(ctx, session, zoneName)
		if outcome.Failed() {
			return zoneDistributionMessage{zoneName: zoneName, err: outcome.Error()}
		}
		return zoneDistributionMessage{zoneName: zoneName, distributed: outcome.Value()}
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

	case transferSubmittedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			var invalid registrar.InvalidDomainError
			if errors.As(message.err, &invalid) {
				browser.screen = screenTransferForm
				return model, nil
			}
			browser.screen = screenDomains
			return model, nil
		}
		browser.status = "✓ " + message.domainName + " transfer accepted - progress polls in the background"
		browser.screen = screenDomains
		return model, nil

	case templatesLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			return model, nil
		}
		browser.templateList = message.templates
		browser.templateCursor = clamp(browser.templateCursor, len(browser.templateList))
		if message.picker {
			browser.screen = screenTemplatePicker
		} else {
			browser.screen = screenTemplates
		}
		return model, nil

	case templateMutatedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			var invalid templates.InvalidTemplateError
			if message.verb != "delete" && errors.As(message.err, &invalid) {
				browser.screen = screenTemplateForm
				return model, nil
			}
			browser.screen = screenTemplates
			return model, nil
		}
		browser.status = "✓ template " + message.verb + " done"
		browser.screen = screenTemplates
		browser.loading = true
		return model, tea.Batch(model.loadTemplates(false), spinnerTick())

	case templateRecordsLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenTemplates
			return model, nil
		}
		browser.currentTemplate = message.template
		browser.templateRecordList = message.records
		browser.templateRecordCursor = clamp(browser.templateRecordCursor, len(browser.templateRecordList))
		browser.screen = screenTemplateRecords
		return model, nil

	case templateRecordMutatedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			var invalid zones.InvalidRecordError
			if message.verb != "delete" && errors.As(message.err, &invalid) {
				browser.screen = screenForm
				return model, nil
			}
			browser.screen = screenTemplateRecords
			return model, nil
		}
		browser.status = "✓ template record " + message.verb + " done"
		browser.screen = screenTemplateRecords
		browser.loading = true
		return model, tea.Batch(model.loadTemplateRecords(browser.currentTemplate), spinnerTick())

	case templateAppliedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenZones
			return model, nil
		}
		browser.status = "✓ template " + message.templateName + " applied to " + browser.currentDomain
		browser.screen = screenZones
		return model, nil

	case webhooksLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenHome
			return model, nil
		}
		browser.webhookList = message.webhooks
		browser.webhookCursor = clamp(browser.webhookCursor, len(browser.webhookList))
		browser.screen = screenWebhooks
		return model, nil

	case webhookMutatedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			var invalid webhooks.InvalidWebhookError
			if message.verb == "create" && errors.As(message.err, &invalid) {
				browser.screen = screenWebhookForm
				return model, nil
			}
			browser.screen = screenWebhooks
			return model, nil
		}
		browser.status = "✓ webhook " + message.verb + " done"
		browser.loading = true
		browser.screen = screenWebhooks
		return model, tea.Batch(model.loadWebhooks(), spinnerTick())

	case zoneActivationMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenZones
			return model, nil
		}
		browser.status = "✓ zone activation updated"
		browser.loading = true
		browser.screen = screenZones
		return model, tea.Batch(model.loadZones(), spinnerTick())

	case zoneDistributionMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			return model, nil
		}
		if message.distributed {
			browser.status = "✓ " + message.zoneName + " is fully distributed across the name servers"
		} else {
			browser.status = "⚠ " + message.zoneName + " is not fully distributed yet - check again shortly"
		}
		return model, nil

	case allCertificatesLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenHome
			return model, nil
		}
		browser.allCertList = message.items
		browser.allCertCursor = clamp(browser.allCertCursor, len(browser.allCertList))
		browser.screen = screenAllCertificates
		return model, nil

	case registrantContactsLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenDomainSettings
			return model, nil
		}
		browser.contactList = message.contacts
		browser.registrantCursor = clamp(browser.registrantCursor, len(browser.contactList))
		browser.screen = screenRegistrantPicker
		return model, nil

	case registrantChangedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenDomainSettings
			return model, nil
		}
		browser.status = "✓ registrant change accepted - registries may email for approval; progress polls in the background"
		browser.screen = screenDomainSettings
		return model, nil

	case servicesLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenZones
			return model, nil
		}
		browser.serviceList = message.applied
		browser.serviceCursor = clamp(browser.serviceCursor, len(browser.serviceList))
		if message.picker {
			browser.availableServices = message.available
			browser.servicePickerCursor = clamp(browser.servicePickerCursor, len(browser.availableServices))
			browser.screen = screenServicePicker
		} else {
			browser.screen = screenDomainServices
		}
		return model, nil

	case serviceChangedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenDomainServices
			return model, nil
		}
		browser.status = "✓ service " + message.verb + " done"
		browser.loading = true
		browser.screen = screenDomainServices
		return model, tea.Batch(model.loadServices(false), spinnerTick())

	case pushesLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenHome
			return model, nil
		}
		browser.pushList = message.pushes
		browser.pushCursor = clamp(browser.pushCursor, len(browser.pushList))
		browser.screen = screenPushes
		return model, nil

	case pushResolvedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			var invalid pushes.InvalidPushError
			if message.verb == "initiate" && errors.As(message.err, &invalid) {
				browser.screen = screenPushForm
				return model, nil
			}
			if message.verb == "initiate" {
				browser.screen = screenDomainSettings
			} else {
				browser.screen = screenPushes
			}
			return model, nil
		}
		if message.verb == "initiate" {
			browser.status = "✓ push initiated - the receiving account accepts or rejects it"
			browser.screen = screenDomainSettings
			return model, nil
		}
		browser.status = "✓ push " + message.verb + " done"
		browser.loading = true
		browser.screen = screenPushes
		return model, tea.Batch(model.loadPushes(), spinnerTick())

	case contactsLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenHome
			return model, nil
		}
		browser.contactList = message.contacts
		browser.contactCursor = clamp(browser.contactCursor, len(browser.contactList))
		browser.screen = screenContacts
		return model, nil

	case contactMutatedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			var invalid contacts.InvalidContactError
			if message.verb != "delete" && errors.As(message.err, &invalid) {
				browser.screen = screenContactForm
				return model, nil
			}
			browser.screen = screenContacts
			return model, nil
		}
		browser.status = "✓ contact " + message.verb + " done"
		browser.screen = screenContacts
		browser.loading = true
		return model, tea.Batch(model.loadContacts(), spinnerTick())

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
		case screenTransferForm:
			return model.transferFormKeys(message)
		case screenContacts:
			return model.contactsKeys(message)
		case screenContactForm:
			return model.contactFormKeys(message)
		case screenTemplates:
			return model.templatesKeys(message)
		case screenTemplateForm:
			return model.templateFormKeys(message)
		case screenTemplateRecords:
			return model.templateRecordsKeys(message)
		case screenTemplatePicker:
			return model.templatePickerKeys(message)
		case screenDomainServices:
			return model.domainServicesKeys(message)
		case screenServicePicker:
			return model.servicePickerKeys(message)
		case screenPushes:
			return model.pushesKeys(message)
		case screenPushForm:
			return model.pushFormKeys(message)
		case screenAllCertificates:
			return model.allCertificatesKeys(message)
		case screenCertDomainPicker:
			return model.certDomainPickerKeys(message)
		case screenRegistrantPicker:
			return model.registrantPickerKeys(message)
		case screenWebhooks:
			return model.webhooksKeys(message)
		case screenWebhookForm:
			return model.webhookFormKeys(message)
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
	case "o":
		browser.confirmPrompt = "Authorize transferring " + browser.currentDomain +
			" OUT of DNSimple? The auth code goes to the registrant by email."
		browser.confirmCommand = model.authorizeTransferOut()
		browser.confirmReturn = screenDomainSettings
		browser.screen = screenConfirmAction
		return model, nil
	case "u":
		browser.confirmPrompt = "Order a restore for " + browser.currentDomain + "? This charges your account."
		browser.confirmCommand = model.restoreDomain()
		browser.confirmReturn = screenDomainSettings
		browser.screen = screenConfirmAction
		return model, nil
	case "c":
		// Changing the registrant needs a contact: load them into a picker.
		browser.loading = true
		browser.status = ""
		browser.registrantCursor = 0
		return model, tea.Batch(model.loadContactsForRegistrant(), spinnerTick())
	case "g":
		input := textinput.New()
		input.Placeholder = "receiving account's email"
		input.CharLimit = 253
		input.Width = 44
		input.Prompt = "> "
		input.PromptStyle = model.styles.selected
		input.Focus()
		browser.pushEmailInput = input
		browser.screen = screenPushForm
		browser.status = ""
		return model, textinput.Blink
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

func (model sessionModel) contactsKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.contactCursor = clamp(browser.contactCursor-1, len(browser.contactList))
	case "down", "j":
		browser.contactCursor = clamp(browser.contactCursor+1, len(browser.contactList))
	case "pgup":
		browser.contactCursor = clamp(browser.contactCursor-model.visibleRows(), len(browser.contactList))
	case "pgdown":
		browser.contactCursor = clamp(browser.contactCursor+model.visibleRows(), len(browser.contactList))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadContacts(), spinnerTick())
	case "n":
		model.openContactForm(nil)
		return model, textinput.Blink
	case "e":
		if len(browser.contactList) == 0 {
			return model, nil
		}
		contact := browser.contactList[browser.contactCursor]
		model.openContactForm(&contact)
		return model, textinput.Blink
	case "d":
		if len(browser.contactList) == 0 {
			return model, nil
		}
		contact := browser.contactList[browser.contactCursor]
		browser.confirmPrompt = "Delete the contact " + contact.Label() +
			"? DNSimple refuses if a domain still uses it."
		browser.confirmCommand = model.deleteContact(contact)
		browser.confirmReturn = screenContacts
		browser.screen = screenConfirmAction
	}
	return model, nil
}

// openContactForm needs the pointer receiver: it mutates the caller's model
// ahead of the return, shared by create (nil) and edit.
func (model *sessionModel) openContactForm(existing *contacts.Contact) {
	browser := &model.browser
	for field := range contactFieldCount {
		input := textinput.New()
		input.Placeholder = contactFieldLabels[field]
		input.CharLimit = 253
		input.Width = 40
		input.Prompt = "  "
		input.PromptStyle = model.styles.selected
		browser.contactForm[field] = input
	}
	browser.editingContactID = 0
	if existing != nil {
		browser.editingContactID = existing.ID
		values := [contactFieldCount]string{
			existing.FirstName, existing.LastName, existing.Email, existing.Phone,
			existing.Address, existing.City, existing.StateProvince,
			existing.PostalCode, existing.Country,
		}
		for field, value := range values {
			browser.contactForm[field].SetValue(value)
		}
	}
	browser.contactForm[0].Prompt = "> "
	browser.contactForm[0].Focus()
	browser.contactFocused = 0
	browser.screen = screenContactForm
	browser.status = ""
}

func (model sessionModel) contactFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenContacts
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.submitContact(), spinnerTick())
	case "tab", "down":
		browser.contactFocused = (browser.contactFocused + 1) % contactFieldCount
	case "shift+tab", "up":
		browser.contactFocused = (browser.contactFocused + contactFieldCount - 1) % contactFieldCount
	default:
		var inputCommand tea.Cmd
		browser.contactForm[browser.contactFocused], inputCommand =
			browser.contactForm[browser.contactFocused].Update(key)
		return model, inputCommand
	}
	for field := range contactFieldCount {
		if field == browser.contactFocused {
			browser.contactForm[field].Prompt = "> "
			browser.contactForm[field].Focus()
		} else {
			browser.contactForm[field].Prompt = "  "
			browser.contactForm[field].Blur()
		}
	}
	return model, nil
}

func (model sessionModel) templatesKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.templateCursor = clamp(browser.templateCursor-1, len(browser.templateList))
	case "down", "j":
		browser.templateCursor = clamp(browser.templateCursor+1, len(browser.templateList))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadTemplates(false), spinnerTick())
	case "n":
		placeholders := [3]string{"name", "short name (lowercase-with-dashes)", "description (optional)"}
		for field := range 3 {
			input := textinput.New()
			input.Placeholder = placeholders[field]
			input.CharLimit = 253
			input.Width = 44
			input.Prompt = "  "
			input.PromptStyle = model.styles.selected
			browser.templateForm[field] = input
		}
		browser.templateForm[0].Prompt = "> "
		browser.templateForm[0].Focus()
		browser.templateFocused = 0
		browser.screen = screenTemplateForm
		browser.status = ""
		return model, textinput.Blink
	case "d":
		if len(browser.templateList) == 0 {
			return model, nil
		}
		template := browser.templateList[browser.templateCursor]
		browser.confirmPrompt = "Delete the template " + template.Name + " and its records?"
		browser.confirmCommand = model.deleteTemplate(template)
		browser.confirmReturn = screenTemplates
		browser.screen = screenConfirmAction
	case "enter":
		if len(browser.templateList) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		browser.templateRecordCursor = 0
		return model, tea.Batch(
			model.loadTemplateRecords(browser.templateList[browser.templateCursor]), spinnerTick())
	}
	return model, nil
}

func (model sessionModel) templateFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenTemplates
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.submitTemplate(), spinnerTick())
	case "tab", "down":
		browser.templateFocused = (browser.templateFocused + 1) % 3
	case "shift+tab", "up":
		browser.templateFocused = (browser.templateFocused + 2) % 3
	default:
		var inputCommand tea.Cmd
		browser.templateForm[browser.templateFocused], inputCommand =
			browser.templateForm[browser.templateFocused].Update(key)
		return model, inputCommand
	}
	for field := range 3 {
		if field == browser.templateFocused {
			browser.templateForm[field].Prompt = "> "
			browser.templateForm[field].Focus()
		} else {
			browser.templateForm[field].Prompt = "  "
			browser.templateForm[field].Blur()
		}
	}
	return model, nil
}

func (model sessionModel) templateRecordsKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenTemplates
		browser.status = ""
	case "up", "k":
		browser.templateRecordCursor = clamp(browser.templateRecordCursor-1, len(browser.templateRecordList))
	case "down", "j":
		browser.templateRecordCursor = clamp(browser.templateRecordCursor+1, len(browser.templateRecordList))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadTemplateRecords(browser.currentTemplate), spinnerTick())
	case "n":
		browser.form = newRecordForm(nil, model.styles)
		browser.formForTemplate = true
		browser.screen = screenForm
		browser.status = ""
		return model, textinput.Blink
	case "d":
		if len(browser.templateRecordList) == 0 {
			return model, nil
		}
		record := browser.templateRecordList[browser.templateRecordCursor]
		browser.confirmPrompt = "Delete the template record " + record.Type + " " + record.Content + "?"
		browser.confirmCommand = model.deleteTemplateRecord(record)
		browser.confirmReturn = screenTemplateRecords
		browser.screen = screenConfirmAction
	}
	return model, nil
}

func (model sessionModel) templatePickerKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenZones
		browser.status = ""
	case "up", "k":
		browser.templateCursor = clamp(browser.templateCursor-1, len(browser.templateList))
	case "down", "j":
		browser.templateCursor = clamp(browser.templateCursor+1, len(browser.templateList))
	case "enter":
		if len(browser.templateList) == 0 {
			return model, nil
		}
		template := browser.templateList[browser.templateCursor]
		browser.confirmPrompt = "Apply the template " + template.Name + " to " + browser.currentDomain +
			"? Its records land in the zone."
		browser.confirmCommand = model.applyTemplate(template)
		browser.confirmReturn = screenZones
		browser.screen = screenConfirmAction
	}
	return model, nil
}

func (model sessionModel) domainServicesKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenZones
		browser.status = ""
	case "up", "k":
		browser.serviceCursor = clamp(browser.serviceCursor-1, len(browser.serviceList))
	case "down", "j":
		browser.serviceCursor = clamp(browser.serviceCursor+1, len(browser.serviceList))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadServices(false), spinnerTick())
	case "a":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadServices(true), spinnerTick())
	case "d":
		if len(browser.serviceList) == 0 {
			return model, nil
		}
		service := browser.serviceList[browser.serviceCursor]
		browser.confirmPrompt = "Remove the service " + service.Name + " from " + browser.currentDomain +
			"? Its records leave the zone."
		browser.confirmCommand = model.unapplyService(service)
		browser.confirmReturn = screenDomainServices
		browser.screen = screenConfirmAction
	}
	return model, nil
}

func (model sessionModel) servicePickerKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenDomainServices
		browser.status = ""
	case "up", "k":
		browser.servicePickerCursor = clamp(browser.servicePickerCursor-1, len(browser.availableServices))
	case "down", "j":
		browser.servicePickerCursor = clamp(browser.servicePickerCursor+1, len(browser.availableServices))
	case "enter":
		if len(browser.availableServices) == 0 {
			return model, nil
		}
		service := browser.availableServices[browser.servicePickerCursor]
		browser.confirmPrompt = "Apply the service " + service.Name + " to " + browser.currentDomain + "?"
		browser.confirmCommand = model.applyService(service)
		browser.confirmReturn = screenDomainServices
		browser.screen = screenConfirmAction
	}
	return model, nil
}

func (model sessionModel) pushesKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.pushCursor = clamp(browser.pushCursor-1, len(browser.pushList))
	case "down", "j":
		browser.pushCursor = clamp(browser.pushCursor+1, len(browser.pushList))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadPushes(), spinnerTick())
	case "a":
		if len(browser.pushList) == 0 {
			return model, nil
		}
		push := browser.pushList[browser.pushCursor]
		browser.confirmPrompt = fmt.Sprintf("Accept push #%d? The domain joins this account, registered to your first contact.", push.ID)
		browser.confirmCommand = model.acceptPush(push)
		browser.confirmReturn = screenPushes
		browser.screen = screenConfirmAction
	case "x":
		if len(browser.pushList) == 0 {
			return model, nil
		}
		push := browser.pushList[browser.pushCursor]
		browser.confirmPrompt = fmt.Sprintf("Reject push #%d?", push.ID)
		browser.confirmCommand = model.rejectPush(push)
		browser.confirmReturn = screenPushes
		browser.screen = screenConfirmAction
	}
	return model, nil
}

func (model sessionModel) pushFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenDomainSettings
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.initiatePush(), spinnerTick())
	}
	var inputCommand tea.Cmd
	browser.pushEmailInput, inputCommand = browser.pushEmailInput.Update(key)
	return model, inputCommand
}

func (model sessionModel) allCertificatesKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.allCertCursor = clamp(browser.allCertCursor-1, len(browser.allCertList))
	case "down", "j":
		browser.allCertCursor = clamp(browser.allCertCursor+1, len(browser.allCertList))
	case "pgup":
		browser.allCertCursor = clamp(browser.allCertCursor-model.visibleRows(), len(browser.allCertList))
	case "pgdown":
		browser.allCertCursor = clamp(browser.allCertCursor+model.visibleRows(), len(browser.allCertList))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadAllCertificates(), spinnerTick())
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
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenAllCertificates
		browser.status = ""
	case "up", "k":
		browser.certDomainCursor = clamp(browser.certDomainCursor-1, len(browser.domainList))
	case "down", "j":
		browser.certDomainCursor = clamp(browser.certDomainCursor+1, len(browser.domainList))
	case "enter":
		if len(browser.domainList) == 0 {
			return model, nil
		}
		browser.currentDomain = browser.domainList[browser.certDomainCursor].Name
		browser.certForm = newCertificateForm(model.styles)
		browser.screen = screenCertificateForm
		browser.status = ""
		return model, textinput.Blink
	}
	return model, nil
}

func (model sessionModel) registrantPickerKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenDomainSettings
		browser.status = ""
	case "up", "k":
		browser.registrantCursor = clamp(browser.registrantCursor-1, len(browser.contactList))
	case "down", "j":
		browser.registrantCursor = clamp(browser.registrantCursor+1, len(browser.contactList))
	case "enter":
		if len(browser.contactList) == 0 {
			return model, nil
		}
		contact := browser.contactList[browser.registrantCursor]
		browser.confirmPrompt = "Change the registrant of " + browser.currentDomain + " to " +
			contact.Label() + "? Registries may email for approval and lock transfers for 60 days."
		browser.confirmCommand = model.changeRegistrant(contact)
		browser.confirmReturn = screenDomainSettings
		browser.screen = screenConfirmAction
	}
	return model, nil
}

func (model sessionModel) webhooksKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.webhookCursor = clamp(browser.webhookCursor-1, len(browser.webhookList))
	case "down", "j":
		browser.webhookCursor = clamp(browser.webhookCursor+1, len(browser.webhookList))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadWebhooks(), spinnerTick())
	case "n":
		input := textinput.New()
		input.Placeholder = "https://example.com/dnsimple-events"
		input.CharLimit = 500
		input.Width = 50
		input.Prompt = "> "
		input.PromptStyle = model.styles.selected
		input.Focus()
		browser.webhookURLInput = input
		browser.screen = screenWebhookForm
		browser.status = ""
		return model, textinput.Blink
	case "d":
		if len(browser.webhookList) == 0 {
			return model, nil
		}
		webhook := browser.webhookList[browser.webhookCursor]
		browser.confirmPrompt = "Delete the webhook " + webhook.URL + "?"
		browser.confirmCommand = model.deleteWebhook(webhook)
		browser.confirmReturn = screenWebhooks
		browser.screen = screenConfirmAction
	}
	return model, nil
}

func (model sessionModel) webhookFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenWebhooks
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.createWebhook(), spinnerTick())
	}
	var inputCommand tea.Cmd
	browser.webhookURLInput, inputCommand = browser.webhookURLInput.Update(key)
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
		case "Certificates":
			browser.screen = screenAllCertificates
			return model, tea.Batch(model.loadAllCertificates(), spinnerTick())
		case "Templates":
			browser.screen = screenTemplates
			return model, tea.Batch(model.loadTemplates(false), spinnerTick())
		case "Contacts":
			browser.screen = screenContacts
			return model, tea.Batch(model.loadContacts(), spinnerTick())
		case "Pushes":
			browser.screen = screenPushes
			return model, tea.Batch(model.loadPushes(), spinnerTick())
		case "Webhooks":
			browser.screen = screenWebhooks
			return model, tea.Batch(model.loadWebhooks(), spinnerTick())
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
		if len(browser.domainList) > 0 {
			if selected := browser.domainList[browser.domainCursor]; selected.State == "hosted" {
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

func (model sessionModel) transferFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenDomains
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.submitTransfer(), spinnerTick())
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
	case "p":
		if len(browser.zoneList) == 0 {
			return model, nil
		}
		browser.currentDomain = browser.zoneList[browser.zoneCursor].Name
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadTemplates(true), spinnerTick())
	case "x":
		if len(browser.zoneList) == 0 {
			return model, nil
		}
		zone := browser.zoneList[browser.zoneCursor]
		if zone.Active {
			browser.confirmPrompt = "Deactivate DNS for " + zone.Name +
				"? DNSimple's name servers stop answering for it."
			browser.confirmCommand = model.setZoneActivation(zone.Name, false)
			browser.confirmReturn = screenZones
			browser.screen = screenConfirmAction
			return model, nil
		}
		browser.loading = true
		return model, tea.Batch(model.setZoneActivation(zone.Name, true), spinnerTick())
	case "D":
		if len(browser.zoneList) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(
			model.checkZoneDistribution(browser.zoneList[browser.zoneCursor].Name), spinnerTick())
	case "v":
		if len(browser.zoneList) == 0 {
			return model, nil
		}
		browser.currentDomain = browser.zoneList[browser.zoneCursor].Name
		browser.loading = true
		browser.status = ""
		browser.serviceCursor = 0
		return model, tea.Batch(model.loadServices(false), spinnerTick())
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
		browser.formForTemplate = false
		browser.screen = screenForm
		browser.status = ""
		return model, textinput.Blink
	case "e":
		if len(browser.records) == 0 {
			return model, nil
		}
		record := browser.records[browser.recordCursor]
		browser.form = newRecordForm(&record, model.styles)
		browser.formForTemplate = false
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
		if browser.formForTemplate {
			browser.screen = screenTemplateRecords
		} else {
			browser.screen = screenRecords
		}
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		if browser.formForTemplate {
			return model, tea.Batch(model.submitTemplateRecord(), spinnerTick())
		}
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
	case screenTransferForm:
		body = model.viewTransferForm()
	case screenContacts:
		body = model.viewContacts()
	case screenContactForm:
		body = model.viewContactForm()
	case screenTemplates:
		body = model.viewTemplates(false)
	case screenTemplatePicker:
		body = model.viewTemplates(true)
	case screenTemplateForm:
		body = model.viewTemplateForm()
	case screenTemplateRecords:
		body = model.viewTemplateRecords()
	case screenDomainServices:
		body = model.viewDomainServices(false)
	case screenServicePicker:
		body = model.viewDomainServices(true)
	case screenPushes:
		body = model.viewPushes()
	case screenPushForm:
		body = model.viewPushForm()
	case screenAllCertificates:
		body = model.viewAllCertificates()
	case screenCertDomainPicker:
		body = model.viewCertDomainPicker()
	case screenRegistrantPicker:
		body = model.viewRegistrantPicker()
	case screenWebhooks:
		body = model.viewWebhooks()
	case screenWebhookForm:
		body = model.viewWebhookForm()
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
		append(lines, model.footer("x dnssec · a renewal · w whois · l lock · e delegation · c registrant · R renew · o out · u restore · g push · n/d forwards · esc back"))...)
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

func (model sessionModel) viewContacts() string {
	browser := model.browser
	lines := []string{model.header("contacts"), ""}
	if len(browser.contactList) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  none; n creates the first one"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.contactCursor, len(browser.contactList), visible)
	for index := start; index < end; index++ {
		contact := browser.contactList[index]
		line := fmt.Sprintf("%-36.36s %s, %s", contact.Label(), contact.City, contact.Country)
		cursor := "  "
		if index == browser.contactCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.contactCursor, len(browser.contactList), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("n new · e edit · d delete · r refresh · esc back · q quit"))...)
}

func (model sessionModel) viewContactForm() string {
	browser := model.browser
	verb := "new contact"
	if browser.editingContactID != 0 {
		verb = fmt.Sprintf("edit contact #%d", browser.editingContactID)
	}
	lines := []string{model.header(verb), ""}
	for field := range contactFieldCount {
		lines = append(lines, fmt.Sprintf("  %-18s %s", contactFieldLabels[field],
			browser.contactForm[field].View()))
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("tab next field · enter save · esc cancel"))...)
}

func (model sessionModel) viewTemplates(picker bool) string {
	browser := model.browser
	title := "templates"
	hints := "enter records · n new · d delete · r refresh · esc back · q quit"
	if picker {
		title = "apply a template to " + browser.currentDomain
		hints = "enter apply · esc cancel"
	}
	lines := []string{model.header(title), ""}
	if len(browser.templateList) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no templates; n creates one from the Templates screen"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.templateCursor, len(browser.templateList), visible)
	for index := start; index < end; index++ {
		template := browser.templateList[index]
		line := fmt.Sprintf("%-24.24s %-20.20s %s", template.Name, template.SID, template.Description)
		cursor := "  "
		if index == browser.templateCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.templateCursor, len(browser.templateList), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left, append(lines, model.footer(hints))...)
}

func (model sessionModel) viewTemplateForm() string {
	browser := model.browser
	labels := [3]string{"Name       ", "Short name ", "Description"}
	lines := []string{model.header("new template"), ""}
	for field := range 3 {
		lines = append(lines, "  "+labels[field]+" "+browser.templateForm[field].View())
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("tab next field · enter create · esc cancel"))...)
}

func (model sessionModel) viewTemplateRecords() string {
	browser := model.browser
	lines := []string{model.header("template " + browser.currentTemplate.Name), ""}
	if len(browser.templateRecordList) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no records; n adds the first one"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.templateRecordCursor, len(browser.templateRecordList), visible)
	for index := start; index < end; index++ {
		record := browser.templateRecordList[index]
		name := record.Name
		if name == "" {
			name = "@"
		}
		line := fmt.Sprintf("%-20.20s %-6s %-32.32s %6d", name, record.Type, record.Content, record.TTL)
		cursor := "  "
		if index == browser.templateRecordCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.templateRecordCursor, len(browser.templateRecordList), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("n new · d delete · r refresh · esc back · q quit"))...)
}

func (model sessionModel) viewDomainServices(picker bool) string {
	browser := model.browser
	title := browser.currentDomain + " · services"
	hints := "a add · d remove · r refresh · esc back · q quit"
	listed := browser.serviceList
	cursor := browser.serviceCursor
	empty := "  none applied; a adds one"
	if picker {
		title = "apply a service to " + browser.currentDomain
		hints = "enter apply · esc cancel"
		listed = browser.availableServices
		cursor = browser.servicePickerCursor
		empty = "  every available service is already applied"
	}
	lines := []string{model.header(title), ""}
	if len(listed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render(empty))
	}
	visible := model.visibleRows()
	start, end := listWindow(cursor, len(listed), visible)
	for index := start; index < end; index++ {
		service := listed[index]
		line := fmt.Sprintf("%-24.24s %s", service.Name, service.Description)
		prefix := "  "
		if index == cursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left, append(lines, model.footer(hints))...)
}

func (model sessionModel) viewPushes() string {
	browser := model.browser
	lines := []string{model.header("incoming pushes"), ""}
	if len(browser.pushList) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  none pending; push a domain here from its settings (g) on the other account"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.pushCursor, len(browser.pushList), visible)
	for index := start; index < end; index++ {
		push := browser.pushList[index]
		line := fmt.Sprintf("push #%-6d domain id %-8d since %s", push.ID, push.DomainID, push.CreatedAt)
		prefix := "  "
		if index == browser.pushCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("a accept · x reject · r refresh · esc back · q quit"))...)
}

func (model sessionModel) viewPushForm() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		model.header("push "+model.browser.currentDomain+" to another account"),
		"",
		"  Email  "+model.browser.pushEmailInput.View(),
		"",
		model.styles.subtle.Render("  the receiving account accepts or rejects the push"),
		model.footer("enter push · esc cancel"),
	)
}

func (model sessionModel) viewAllCertificates() string {
	browser := model.browser
	lines := []string{model.header("certificates"), ""}
	if len(browser.allCertList) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  none across the portfolio; i requests one"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.allCertCursor, len(browser.allCertList), visible)
	for index := start; index < end; index++ {
		item := browser.allCertList[index]
		line := fmt.Sprintf("%-24.24s %-28.28s %-12s expires %s",
			item.Domain, item.Certificate.CommonName, item.Certificate.State, item.Certificate.ExpiresAt)
		prefix := "  "
		if index == browser.allCertCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	if position := model.positionLine(browser.allCertCursor, len(browser.allCertList), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("i issue · r refresh · esc back · q quit"))...)
}

func (model sessionModel) viewCertDomainPicker() string {
	browser := model.browser
	lines := []string{model.header("issue a certificate for..."), ""}
	visible := model.visibleRows()
	start, end := listWindow(browser.certDomainCursor, len(browser.domainList), visible)
	for index := start; index < end; index++ {
		line := browser.domainList[index].Name
		prefix := "  "
		if index == browser.certDomainCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("enter pick · esc cancel"))...)
}

func (model sessionModel) viewRegistrantPicker() string {
	browser := model.browser
	lines := []string{model.header("new registrant for " + browser.currentDomain), ""}
	if len(browser.contactList) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no contacts; create one in the Contacts screen first"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.registrantCursor, len(browser.contactList), visible)
	for index := start; index < end; index++ {
		line := browser.contactList[index].Label()
		prefix := "  "
		if index == browser.registrantCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("enter pick · esc cancel"))...)
}

func (model sessionModel) viewWebhooks() string {
	browser := model.browser
	lines := []string{model.header("webhooks"), ""}
	if len(browser.webhookList) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  none; n registers an https endpoint for account events"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.webhookCursor, len(browser.webhookList), visible)
	for index := start; index < end; index++ {
		line := browser.webhookList[index].URL
		prefix := "  "
		if index == browser.webhookCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("n new · d delete · r refresh · esc back · q quit"))...)
}

func (model sessionModel) viewWebhookForm() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		model.header("new webhook"),
		"",
		"  URL  "+model.browser.webhookURLInput.View(),
		"",
		model.styles.subtle.Render("  DNSimple posts every account event to this https endpoint"),
		model.footer("enter create · esc cancel"),
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
		append(lines, model.footer("enter certs · s settings · n register · t transfer · c cert · r refresh · esc back · q quit"))...)
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
		zone := browser.zoneList[index]
		state := "active"
		if !zone.Active {
			state = "inactive"
		}
		line := fmt.Sprintf("%-32.32s %s", zone.Name, state)
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
		append(lines, model.footer("enter open · v services · p template · x activation · D distribution · r refresh · esc back · q quit"))...)
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
		return "✗ your account has no contacts - create one in the Contacts screen, then retry"
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
