package sshui

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"dnsshimple/core/billing"
	"dnsshimple/core/certificates"
	"dnsshimple/core/contacts"
	"dnsshimple/core/domains"
	"dnsshimple/core/gateway"
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
	screenDSRecords
	screenDSRecordForm
	screenRegistrantChanges
	screenAddDomainForm
	screenNSRecordsForm
	screenExtAttrForm
	screenAccountPicker
	screenSecondaryDNS
	screenPrimaryForm
	screenSecondaryZoneForm
	screenAnalytics
	screenTLDs
	screenTextView
	screenConfirmAction
)

var homeEntries = []string{"Zones", "Domains", "Certificates", "Templates", "Contacts", "Pushes", "Webhooks", "Secondary DNS", "Analytics", "TLDs", "Billing"}

type browserState struct {
	screen             appScreen
	loading            bool
	status             string
	homeCursor         int
	zoneList           []zones.Zone
	zoneCursor         int
	currentZone        string
	records            []zones.Record
	recordCursor       int
	form               recordForm
	deleteTarget       *zones.Record
	primaryList        []secondarydns.PrimaryServer
	primaryCursor      int
	secondaryZoneNames []string
	primaryForm        [3]textinput.Model // name, ip, port
	primaryFocused     int
	secondaryZoneInput textinput.Model
	volumeRows         []insights.VolumeRow
	analyticsCursor    int
	tldList            []insights.TLD
	tldCursor          int

	webhookList     []webhooks.Webhook
	webhookCursor   int
	webhookURLInput textinput.Model

	actionFocus       int // -1 = list focused; otherwise an index into screenActions
	allCertList       []certificateOverview
	allCertCursor     int
	listFilter        textinput.Model
	listFilterOn      bool
	listFilterScreen  appScreen
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

	dsRecordList   []domains.DSRecord
	dsRecordCursor int
	dsForm         [4]textinput.Model

	registrantChangeList   []registrar.RegistrantChange
	registrantChangeCursor int

	accountList   []identity.Tenant
	accountCursor int

	extAttrDefs    []insights.ExtendedAttribute
	extAttrForm    []textinput.Model
	extAttrFocused int
	extAttrAction  string // "register" or "transfer"

	// partyMode is the Konami-code easter egg: random colors per render.
	partyMode   bool
	cheatBuffer []string
	// pendingTransfers is session-local: DNSimple has no list-transfers
	// endpoint, so cancellation is only offered for transfers started here.
	pendingTransfers  []pendingTransfer
	dsFocused         int
	editingTemplateID int64

	// generic scrollable text view
	textTitle  string
	textLines  []string
	textOffset int
	textReturn appScreen

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
	return browserState{screen: screenHome, actionFocus: -1}
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
	transfer   registrar.Transfer
	err        error
}

type pendingTransfer struct {
	domain   string
	transfer registrar.Transfer
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

func (model sessionModel) submitRegistration(extendedAttributes map[string]string) tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := strings.ToLower(strings.TrimSpace(model.browser.registerInput.Value()))
	return func() tea.Msg {
		outcome := component.RegisterDomain(ctx, session, domainName, extendedAttributes)
		return registrationSubmittedMessage{domainName: domainName, err: outcome.Error()}
	}
}

type registryDemandsMessage struct {
	action     string
	attributes []insights.ExtendedAttribute
}

// checkRegistryDemands asks the TLD's attribute registry what the write
// will need; nothing demanded means the write fires immediately.
func (model sessionModel) checkRegistryDemands(action, domainName string) tea.Cmd {
	insightsComponent, session, ctx := model.components.Insights, model.session, model.appCtx
	submit := model.submitRegistration(nil)
	if action == "transfer" {
		submit = model.submitTransfer(nil)
	}
	dot := strings.Index(domainName, ".")
	if dot < 0 {
		// An invalid name skips the registry and fails in the component
		// with its usual typed error.
		return submit
	}
	tld := domainName[dot+1:]
	return func() tea.Msg {
		outcome := insightsComponent.ExtendedAttributes(ctx, session, tld)
		if outcome.Failed() || len(outcome.Value()) == 0 {
			// A registry lookup failure falls through to the write, whose
			// own error handling reports what the registry demanded.
			return submit()
		}
		return registryDemandsMessage{action: action, attributes: outcome.Value()}
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

func (model sessionModel) submitTransfer(extendedAttributes map[string]string) tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := strings.ToLower(strings.TrimSpace(model.browser.transferForm[0].Value()))
	authCode := model.browser.transferForm[1].Value()
	return func() tea.Msg {
		outcome := component.TransferDomain(ctx, session, domainName, authCode, extendedAttributes)
		return transferSubmittedMessage{domainName: domainName, transfer: outcome.Value(), err: outcome.Error()}
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

func (model sessionModel) delegateToVanity() tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		nameServers := make([]string, 0, 4)
		for index := 1; index <= 4; index++ {
			nameServers = append(nameServers, fmt.Sprintf("ns%d.%s", index, domainName))
		}
		outcome := component.DelegateToVanity(ctx, session, domainName, nameServers)
		return settingChangedMessage{what: "vanity delegation", err: outcome.Error()}
	}
}

func (model sessionModel) dedelegateFromVanity() tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.DedelegateFromVanity(ctx, session, domainName)
		return settingChangedMessage{what: "vanity undelegation", err: outcome.Error()}
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
		ID:          model.browser.editingTemplateID,
		Name:        model.browser.templateForm[0].Value(),
		SID:         model.browser.templateForm[1].Value(),
		Description: model.browser.templateForm[2].Value(),
	}
	return func() tea.Msg {
		if draft.ID != 0 {
			outcome := component.UpdateTemplate(ctx, session, draft)
			return templateMutatedMessage{verb: "update", err: outcome.Error()}
		}
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

type secondaryLoadedMessage struct {
	primaries []secondarydns.PrimaryServer
	zones     []string
	err       error
}

type primaryMutatedMessage struct {
	verb string
	err  error
}

type analyticsLoadedMessage struct {
	rows []insights.VolumeRow
	err  error
}

type tldsLoadedMessage struct {
	tlds []insights.TLD
	err  error
}

type tldPriceMessage struct {
	tld   string
	price float64
	err   error
}

func (model sessionModel) loadSecondaryDNS() tea.Cmd {
	component, session, ctx := model.components.SecondaryDNS, model.session, model.appCtx
	return func() tea.Msg {
		primaries := component.Primaries(ctx, session)
		if primaries.Failed() {
			return secondaryLoadedMessage{err: primaries.Error()}
		}
		zones := component.SecondaryZones(ctx, session)
		if zones.Failed() {
			return secondaryLoadedMessage{err: zones.Error()}
		}
		return secondaryLoadedMessage{primaries: primaries.Value(), zones: zones.Value()}
	}
}

func (model sessionModel) submitPrimary() tea.Cmd {
	component, session, ctx := model.components.SecondaryDNS, model.session, model.appCtx
	name := model.browser.primaryForm[0].Value()
	ip := model.browser.primaryForm[1].Value()
	port, err := strconv.Atoi(strings.TrimSpace(model.browser.primaryForm[2].Value()))
	if err != nil {
		return func() tea.Msg {
			return primaryMutatedMessage{verb: "create",
				err: secondarydns.InvalidPrimaryError{Reason: "the port must be a number"}}
		}
	}
	return func() tea.Msg {
		outcome := component.CreatePrimary(ctx, session, name, ip, port)
		return primaryMutatedMessage{verb: "create", err: outcome.Error()}
	}
}

func (model sessionModel) deletePrimary(primary secondarydns.PrimaryServer) tea.Cmd {
	component, session, ctx := model.components.SecondaryDNS, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.DeletePrimary(ctx, session, primary.ID)
		return primaryMutatedMessage{verb: "delete", err: outcome.Error()}
	}
}

func (model sessionModel) setPrimaryLink(primary secondarydns.PrimaryServer, linked bool) tea.Cmd {
	component, session, ctx := model.components.SecondaryDNS, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.SetPrimaryLink(ctx, session, primary.ID, linked)
		return primaryMutatedMessage{verb: "link change", err: outcome.Error()}
	}
}

func (model sessionModel) submitSecondaryZone() tea.Cmd {
	component, session, ctx := model.components.SecondaryDNS, model.session, model.appCtx
	zoneName := model.browser.secondaryZoneInput.Value()
	return func() tea.Msg {
		outcome := component.CreateSecondaryZone(ctx, session, zoneName)
		return primaryMutatedMessage{verb: "secondary zone create", err: outcome.Error()}
	}
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
	component, session, ctx := model.components.Insights, model.session, model.appCtx
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
	component, session, ctx := model.components.Insights, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.TLDs(ctx, session)
		if outcome.Failed() {
			return tldsLoadedMessage{err: outcome.Error()}
		}
		return tldsLoadedMessage{tlds: outcome.Value()}
	}
}

type quoteMessage struct {
	domainName string
	quote      registrar.Quote
	err        error
}

type textLoadedMessage struct {
	title  string
	text   string
	back   appScreen
	err    error
	secret bool // secrets show with a reminder, never in the status line
}

func (model sessionModel) quoteDomain() tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := strings.ToLower(strings.TrimSpace(model.browser.registerInput.Value()))
	return func() tea.Msg {
		outcome := component.QuoteDomain(ctx, session, domainName)
		return quoteMessage{domainName: domainName, quote: outcome.Value(), err: outcome.Error()}
	}
}

func (model sessionModel) loadZoneFile(zoneName string) tea.Cmd {
	component, session, ctx := model.components.Zones, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.ZoneFile(ctx, session, zoneName)
		return textLoadedMessage{title: zoneName + " · zone file", text: outcome.Value(),
			back: screenZones, err: outcome.Error()}
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
type action struct {
	key   string
	label string
}

// screenActions drives the tab-navigable footers. Form screens are absent
// on purpose: they own tab for field navigation.
var screenActions = map[appScreen][]action{
	screenHome:              {{"enter", "open"}, {"w", "accounts"}, {"q", "quit"}},
	screenAccountPicker:     {{"enter", "switch"}, {"n", "link another"}, {"esc", "back"}},
	screenZones:             {{"/", "filter"}, {"enter", "open"}, {"v", "services"}, {"p", "template"}, {"x", "activation"}, {"D", "distribution"}, {"f", "file"}, {"r", "refresh"}, {"esc", "back"}},
	screenRecords:           {{"/", "filter"}, {"n", "new"}, {"e", "edit"}, {"d", "delete"}, {"D", "distribution"}, {"N", "ns records"}, {"r", "refresh"}, {"esc", "back"}},
	screenDomains:           {{"/", "filter"}, {"enter", "certs"}, {"s", "settings"}, {"n", "register"}, {"a", "add"}, {"d", "delete"}, {"t", "transfer"}, {"c", "certificate"}, {"h", "changes"}, {"r", "refresh"}, {"esc", "back"}},
	screenCertificates:      {{"/", "filter"}, {"i", "issue"}, {"R", "renew"}, {"w", "chain"}, {"K", "private key"}, {"r", "refresh"}, {"esc", "back"}},
	screenAllCertificates:   {{"/", "filter"}, {"i", "issue"}, {"w", "chain"}, {"K", "private key"}, {"r", "refresh"}, {"esc", "back"}},
	screenDomainSettings:    {{"x", "dnssec"}, {"a", "renewal"}, {"w", "whois"}, {"l", "lock"}, {"e", "delegation"}, {"c", "registrant"}, {"R", "renew"}, {"o", "transfer out"}, {"u", "restore"}, {"g", "push"}, {"D", "ds records"}, {"y", "vanity on"}, {"Y", "vanity off"}, {"v", "vanity delegation"}, {"V", "vanity undelegation"}, {"n", "new forward"}, {"d", "delete forward"}, {"esc", "back"}},
	screenTemplates:         {{"/", "filter"}, {"enter", "records"}, {"n", "new"}, {"e", "edit"}, {"d", "delete"}, {"r", "refresh"}, {"esc", "back"}},
	screenTemplateRecords:   {{"/", "filter"}, {"n", "new"}, {"d", "delete"}, {"r", "refresh"}, {"esc", "back"}},
	screenTemplatePicker:    {{"/", "filter"}, {"enter", "apply"}, {"esc", "cancel"}},
	screenDomainServices:    {{"/", "filter"}, {"a", "add"}, {"d", "remove"}, {"r", "refresh"}, {"esc", "back"}},
	screenServicePicker:     {{"/", "filter"}, {"enter", "apply"}, {"esc", "cancel"}},
	screenContacts:          {{"/", "filter"}, {"n", "new"}, {"e", "edit"}, {"d", "delete"}, {"r", "refresh"}, {"esc", "back"}},
	screenPushes:            {{"/", "filter"}, {"a", "accept"}, {"x", "reject"}, {"r", "refresh"}, {"esc", "back"}},
	screenWebhooks:          {{"/", "filter"}, {"n", "new"}, {"d", "delete"}, {"r", "refresh"}, {"esc", "back"}},
	screenSecondaryDNS:      {{"/", "filter"}, {"n", "new primary"}, {"l", "link/unlink"}, {"z", "new secondary zone"}, {"d", "delete"}, {"r", "refresh"}, {"esc", "back"}},
	screenAnalytics:         {{"/", "filter"}, {"r", "refresh"}, {"esc", "back"}},
	screenDSRecords:         {{"/", "filter"}, {"n", "new"}, {"d", "delete"}, {"r", "refresh"}, {"esc", "back"}},
	screenRegistrantChanges: {{"/", "filter"}, {"x", "cancel"}, {"r", "refresh"}, {"esc", "back"}},
	screenTLDs:              {{"/", "filter"}, {"enter", "price"}, {"a", "attributes"}, {"r", "refresh"}, {"esc", "back"}},
	screenBilling:           {{"/", "filter"}, {"r", "refresh"}, {"esc", "back"}},
	screenCertDomainPicker:  {{"/", "filter"}, {"enter", "pick"}, {"esc", "cancel"}},
	screenRegistrantPicker:  {{"/", "filter"}, {"enter", "pick"}, {"esc", "cancel"}},
}

// actionFooter renders the interactive footer: actions wrap into rows at a
// fixed width instead of stretching the frame, and the focused one is
// highlighted for enter to execute.
func (model sessionModel) actionFooter() string {
	const maxFooterWidth = 68
	actions := screenActions[model.browser.screen]
	lines := []string{""}
	if model.browser.loading {
		lines = append(lines, spinnerFrames[model.spinnerFrame]+" talking to DNSimple...")
	}
	var row strings.Builder
	rowWidth := 0
	flush := func() {
		if rowWidth > 0 {
			lines = append(lines, row.String())
			row.Reset()
			rowWidth = 0
		}
	}
	for index, entry := range actions {
		item := entry.key + " " + entry.label
		if rowWidth > 0 && rowWidth+3+len(item) > maxFooterWidth {
			flush()
		}
		if rowWidth > 0 {
			row.WriteString(model.styles.subtle.Render(" · "))
			rowWidth += 3
		}
		if index == model.browser.actionFocus {
			row.WriteString(model.styles.selected.Render(item))
		} else {
			row.WriteString(model.styles.subtle.Render(item))
		}
		rowWidth += len(item)
	}
	flush()
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// filterableScreens lists every screen whose list takes the / filter.
var filterableScreens = map[appScreen]bool{
	screenZones: true, screenRecords: true, screenDomains: true,
	screenCertificates: true, screenAllCertificates: true,
	screenTemplates: true, screenTemplateRecords: true, screenTemplatePicker: true,
	screenDomainServices: true, screenServicePicker: true,
	screenContacts: true, screenPushes: true, screenWebhooks: true,
	screenSecondaryDNS: true, screenAnalytics: true, screenTLDs: true,
	screenBilling: true, screenCertDomainPicker: true, screenRegistrantPicker: true,
	screenDSRecords: true, screenRegistrantChanges: true,
}

func (model sessionModel) filterQuery() string {
	if !model.browser.listFilterOn {
		return ""
	}
	return strings.TrimSpace(model.browser.listFilter.Value())
}

// filterItems is the one filter every list shares: fuzzy-match the query
// against each item's search text.
func filterItems[Item any](model sessionModel, items []Item, text func(Item) string) []Item {
	query := model.filterQuery()
	if query == "" {
		return items
	}
	var matched []Item
	for _, item := range items {
		if fuzzyMatch(query, text(item)) {
			matched = append(matched, item)
		}
	}
	return matched
}

func (model sessionModel) filteredZones() []zones.Zone {
	return filterItems(model, model.browser.zoneList, func(zone zones.Zone) string { return zone.Name })
}

func (model sessionModel) filteredRecords() []zones.Record {
	return filterItems(model, model.browser.records, func(record zones.Record) string {
		return record.Name + " " + record.Type + " " + record.Content
	})
}

func (model sessionModel) filteredDomains() []domains.Domain {
	return filterItems(model, model.browser.domainList, func(domain domains.Domain) string {
		return domain.Name + " " + domain.State
	})
}

func (model sessionModel) filteredCertificates() []certificates.Certificate {
	return filterItems(model, model.browser.certificateList, func(certificate certificates.Certificate) string {
		return certificate.CommonName + " " + certificate.State
	})
}

func (model sessionModel) filteredTemplates() []templates.Template {
	return filterItems(model, model.browser.templateList, func(template templates.Template) string {
		return template.Name + " " + template.SID + " " + template.Description
	})
}

func (model sessionModel) filteredTemplateRecords() []zones.Record {
	return filterItems(model, model.browser.templateRecordList, func(record zones.Record) string {
		return record.Name + " " + record.Type + " " + record.Content
	})
}

func (model sessionModel) filteredContacts() []contacts.Contact {
	return filterItems(model, model.browser.contactList, func(contact contacts.Contact) string {
		return contact.Label() + " " + contact.City + " " + contact.Country
	})
}

func (model sessionModel) filteredPushes() []pushes.Push {
	return filterItems(model, model.browser.pushList, func(push pushes.Push) string {
		return fmt.Sprintf("%d %d", push.ID, push.DomainID)
	})
}

func (model sessionModel) filteredWebhooks() []webhooks.Webhook {
	return filterItems(model, model.browser.webhookList, func(webhook webhooks.Webhook) string { return webhook.URL })
}

func (model sessionModel) filteredPrimaries() []secondarydns.PrimaryServer {
	return filterItems(model, model.browser.primaryList, func(primary secondarydns.PrimaryServer) string {
		return primary.Name + " " + primary.IP
	})
}

func (model sessionModel) filteredVolumeRows() []insights.VolumeRow {
	return filterItems(model, model.browser.volumeRows, func(row insights.VolumeRow) string { return row.ZoneName })
}

func (model sessionModel) filteredCharges() []billing.Charge {
	return filterItems(model, model.browser.chargeList, func(charge billing.Charge) string {
		return charge.Reference + " " + charge.State
	})
}

func (model sessionModel) filteredServices(picker bool) []services.Service {
	listed := model.browser.serviceList
	if picker {
		listed = model.browser.availableServices
	}
	return filterItems(model, listed, func(service services.Service) string {
		return service.Name + " " + service.Description
	})
}

// clampListCursor keeps the active screen's cursor inside its filtered
// list after every filter keystroke.
func (model *sessionModel) clampListCursor() {
	browser := &model.browser
	switch browser.screen {
	case screenZones:
		browser.zoneCursor = clamp(browser.zoneCursor, len(model.filteredZones()))
	case screenRecords:
		browser.recordCursor = clamp(browser.recordCursor, len(model.filteredRecords()))
	case screenDomains:
		browser.domainCursor = clamp(browser.domainCursor, len(model.filteredDomains()))
	case screenCertificates:
		browser.certificateCursor = clamp(browser.certificateCursor, len(model.filteredCertificates()))
	case screenAllCertificates:
		browser.allCertCursor = clamp(browser.allCertCursor, len(model.filteredAllCerts()))
	case screenTemplates, screenTemplatePicker:
		browser.templateCursor = clamp(browser.templateCursor, len(model.filteredTemplates()))
	case screenTemplateRecords:
		browser.templateRecordCursor = clamp(browser.templateRecordCursor, len(model.filteredTemplateRecords()))
	case screenDomainServices:
		browser.serviceCursor = clamp(browser.serviceCursor, len(model.filteredServices(false)))
	case screenServicePicker:
		browser.servicePickerCursor = clamp(browser.servicePickerCursor, len(model.filteredServices(true)))
	case screenContacts:
		browser.contactCursor = clamp(browser.contactCursor, len(model.filteredContacts()))
	case screenPushes:
		browser.pushCursor = clamp(browser.pushCursor, len(model.filteredPushes()))
	case screenWebhooks:
		browser.webhookCursor = clamp(browser.webhookCursor, len(model.filteredWebhooks()))
	case screenSecondaryDNS:
		browser.primaryCursor = clamp(browser.primaryCursor, len(model.filteredPrimaries()))
	case screenAnalytics:
		browser.analyticsCursor = clamp(browser.analyticsCursor, len(model.filteredVolumeRows()))
	case screenTLDs:
		browser.tldCursor = clamp(browser.tldCursor, len(model.filteredTLDs()))
	case screenCertDomainPicker:
		browser.certDomainCursor = clamp(browser.certDomainCursor, len(model.filteredDomains()))
	case screenRegistrantPicker:
		browser.registrantCursor = clamp(browser.registrantCursor, len(model.filteredContacts()))
	case screenDSRecords:
		browser.dsRecordCursor = clamp(browser.dsRecordCursor, len(model.filteredDSRecords()))
	case screenRegistrantChanges:
		browser.registrantChangeCursor = clamp(browser.registrantChangeCursor, len(model.filteredChangeRows()))
	}
}

// keyMsgFor synthesizes the key event an action stands for, so executing a
// focused action reuses the exact handler its shortcut does.
func keyMsgFor(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

// --- update ----------------------------------------------------------------

func (model sessionModel) updateBrowser(message tea.Msg) (tea.Model, tea.Cmd) {
	browser := &model.browser

	// A filter belongs to the screen it was opened on; navigating away
	// clears it instead of silently filtering the next list.
	if browser.listFilterOn && browser.screen != browser.listFilterScreen {
		browser.listFilterOn = false
		browser.listFilter.SetValue("")
	}

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

	case quoteMessage:
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
		if !message.quote.Available {
			browser.status = "✗ " + message.domainName + " is taken - try another name"
			browser.screen = screenRegisterForm
			return model, nil
		}
		premium := ""
		if message.quote.Premium {
			premium = " (premium)"
		}
		browser.confirmPrompt = fmt.Sprintf("%s is available%s for $%.2f/yr. Register it? This charges your account.",
			message.domainName, premium, message.quote.RegistrationPrice)
		browser.confirmCommand = model.checkRegistryDemands("register", message.domainName)
		browser.confirmReturn = screenDomains
		browser.screen = screenConfirmAction
		return model, nil

	case textLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			return model, nil
		}
		browser.textTitle = message.title
		browser.textLines = strings.Split(message.text, "\n")
		browser.textOffset = 0
		browser.textReturn = message.back
		if message.secret {
			browser.status = "⚠ private key on screen - clear your scrollback afterwards"
		}
		browser.screen = screenTextView
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

	case dsRecordsLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenDomainSettings
			return model, nil
		}
		browser.dsRecordList = message.records
		browser.dsRecordCursor = clamp(browser.dsRecordCursor, len(browser.dsRecordList))
		browser.screen = screenDSRecords
		return model, nil

	case nsRecordsUpdatedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			var invalid zones.InvalidRecordError
			if errors.As(message.err, &invalid) {
				browser.screen = screenNSRecordsForm
				return model, nil
			}
			browser.screen = screenRecords
			return model, nil
		}
		browser.status = "✓ ns records replaced"
		browser.loading = true
		browser.screen = screenRecords
		return model, tea.Batch(model.loadRecords(browser.currentZone), spinnerTick())

	case certificateRenewedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			return model, nil
		}
		browser.status = "✓ " + message.commonName + " renewal accepted - the new certificate polls in the background"
		browser.loading = true
		return model, tea.Batch(model.loadCertificates(browser.currentDomain), spinnerTick())

	case domainMutatedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			var invalid domains.InvalidDomainNameError
			if message.verb == "added" && errors.As(message.err, &invalid) {
				browser.screen = screenAddDomainForm
				return model, nil
			}
			browser.screen = screenDomains
			return model, nil
		}
		browser.status = "✓ " + message.name + " " + message.verb
		browser.loading = true
		browser.screen = screenDomains
		return model, tea.Batch(model.loadDomains(), spinnerTick())

	case relinkStartedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			return model, nil
		}
		// Back to the waiting screen: the browser handshake for the new
		// account lands through the same LISTEN/NOTIFY door as first link.
		screen := waitingScreen{
			identity:    model.components.Identity,
			ctx:         model.appCtx,
			fingerprint: model.fingerprint,
			linkPageURL: model.linkPageURL,
			pending:     message.pending,
		}
		model.state = stateWaiting
		model.waiting = &screen
		return model, tea.Batch(awaitHandshake(screen), spinnerTick())

	case linkedAccountsMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			return model, nil
		}
		browser.accountList = message.tenants
		browser.accountCursor = 0
		browser.screen = screenAccountPicker
		return model, nil

	case accountSwitchedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			return model, nil
		}
		// Every cached list belongs to the old tenant; reset, keep the mood.
		party := browser.partyMode
		model.browser = newBrowserState()
		model.browser.partyMode = party
		model.browser.status = fmt.Sprintf("✓ switched to account #%d", message.session.Tenant.AccountID)
		model.session = message.session
		return model, nil

	case registryDemandsMessage:
		browser.loading = false
		browser.extAttrDefs = message.attributes
		browser.extAttrAction = message.action
		browser.extAttrForm = make([]textinput.Model, len(message.attributes))
		for index, attribute := range message.attributes {
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

	case registrantChangesLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			return model, nil
		}
		browser.registrantChangeList = message.changes
		browser.registrantChangeCursor = clamp(browser.registrantChangeCursor, len(browser.registrantChangeList)+len(browser.pendingTransfers))
		browser.screen = screenRegistrantChanges
		return model, nil

	case changeCancelledMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenRegistrantChanges
			return model, nil
		}
		if message.kind == "transfer" {
			// Transfers have no list endpoint to refresh from, so the
			// session-local copy records the terminal state itself.
			for index := range browser.pendingTransfers {
				if browser.pendingTransfers[index].transfer.ID == message.id {
					browser.pendingTransfers[index].transfer.State = registrar.TransferCancelled
				}
			}
		}
		browser.status = fmt.Sprintf("✓ %s #%d cancelled", message.kind, message.id)
		browser.loading = true
		browser.screen = screenRegistrantChanges
		return model, tea.Batch(model.loadRegistrantChanges(), spinnerTick())

	case dsRecordMutatedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			var invalid domains.InvalidForwardError
			if message.verb == "create" && errors.As(message.err, &invalid) {
				browser.screen = screenDSRecordForm
				return model, nil
			}
			browser.screen = screenDSRecords
			return model, nil
		}
		browser.status = "✓ ds record " + message.verb + " done"
		browser.loading = true
		browser.screen = screenDSRecords
		return model, tea.Batch(model.loadDSRecords(), spinnerTick())

	case recordDistributionMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			return model, nil
		}
		if message.distributed {
			browser.status = "✓ " + message.label + " is fully distributed"
		} else {
			browser.status = "⚠ " + message.label + " is not fully distributed yet"
		}
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
		browser.pendingTransfers = append(browser.pendingTransfers, pendingTransfer{domain: message.domainName, transfer: message.transfer})
		browser.status = fmt.Sprintf("✓ %s transfer #%d accepted - polls in the background; cancel from h changes", message.domainName, message.transfer.ID)
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

	case secondaryLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenHome
			return model, nil
		}
		browser.primaryList = message.primaries
		browser.secondaryZoneNames = message.zones
		browser.primaryCursor = clamp(browser.primaryCursor, len(browser.primaryList))
		browser.screen = screenSecondaryDNS
		return model, nil

	case primaryMutatedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			var invalid secondarydns.InvalidPrimaryError
			if message.verb == "create" && errors.As(message.err, &invalid) {
				browser.screen = screenPrimaryForm
				return model, nil
			}
			browser.screen = screenSecondaryDNS
			return model, nil
		}
		browser.status = "✓ " + message.verb + " done"
		browser.loading = true
		browser.screen = screenSecondaryDNS
		return model, tea.Batch(model.loadSecondaryDNS(), spinnerTick())

	case analyticsLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenHome
			return model, nil
		}
		browser.volumeRows = message.rows
		browser.analyticsCursor = clamp(browser.analyticsCursor, len(browser.volumeRows))
		browser.screen = screenAnalytics
		return model, nil

	case tldPriceMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			return model, nil
		}
		browser.status = fmt.Sprintf("✓ .%s registration: $%.2f/yr", message.tld, message.price)
		return model, nil

	case tldsLoadedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenHome
			return model, nil
		}
		browser.tldList = message.tlds
		browser.tldCursor = clamp(browser.tldCursor, len(browser.tldList))
		browser.screen = screenTLDs
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
		// The live filter: typing edits the query, esc closes it, and
		// navigation/enter fall through to the screen handler, which
		// already operates on the filtered list.
		if browser.listFilterOn && filterableScreens[browser.screen] {
			switch message.String() {
			case "esc":
				browser.listFilterOn = false
				browser.listFilter.SetValue("")
				model.clampListCursor()
				return model, nil
			case "enter", "up", "down", "pgup", "pgdown", "tab", "shift+tab":
				// fall through to the screen handler / action focus below
			default:
				var inputCommand tea.Cmd
				browser.listFilter, inputCommand = browser.listFilter.Update(message)
				model.clampListCursor()
				return model, inputCommand
			}
		} else if message.String() == "/" && filterableScreens[browser.screen] {
			input := textinput.New()
			input.Placeholder = "type to filter, esc clears"
			input.CharLimit = 63
			input.Width = 30
			input.Prompt = "/ "
			input.PromptStyle = model.styles.selected
			input.Focus()
			browser.listFilter = input
			browser.listFilterOn = true
			browser.listFilterScreen = browser.screen
			browser.status = ""
			model.clampListCursor()
			return model, textinput.Blink
		}
		// The interactive footer: tab cycles action focus, enter executes
		// the focused action against the current list selection, up/down
		// hand focus back to the list. Filters and forms keep their keys.
		if actions, actionable := screenActions[browser.screen]; actionable {
			switch message.String() {
			case "tab":
				browser.actionFocus = (browser.actionFocus + 1) % len(actions)
				return model, nil
			case "shift+tab":
				if browser.actionFocus < 0 {
					browser.actionFocus = len(actions) - 1
				} else {
					browser.actionFocus = (browser.actionFocus + len(actions) - 1) % len(actions)
				}
				return model, nil
			case "up", "down", "k", "j":
				if browser.actionFocus >= 0 {
					browser.actionFocus = -1
					return model, nil
				}
			case "enter":
				if browser.actionFocus >= 0 && browser.actionFocus < len(actions) {
					focused := actions[browser.actionFocus].key
					browser.actionFocus = -1
					return model.updateBrowser(keyMsgFor(focused))
				}
			default:
				browser.actionFocus = -1
			}
		}
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
		case screenDSRecords:
			return model.dsRecordsKeys(message)
		case screenDSRecordForm:
			return model.dsRecordFormKeys(message)
		case screenRegistrantChanges:
			return model.registrantChangesKeys(message)
		case screenAddDomainForm:
			return model.addDomainFormKeys(message)
		case screenNSRecordsForm:
			return model.nsRecordsFormKeys(message)
		case screenExtAttrForm:
			return model.extAttrFormKeys(message)
		case screenAccountPicker:
			return model.accountPickerKeys(message)
		case screenSecondaryDNS:
			return model.secondaryDNSKeys(message)
		case screenPrimaryForm:
			return model.primaryFormKeys(message)
		case screenSecondaryZoneForm:
			return model.secondaryZoneFormKeys(message)
		case screenAnalytics:
			return model.analyticsKeys(message)
		case screenTLDs:
			return model.tldsKeys(message)
		case screenTextView:
			return model.textViewKeys(message)
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
	case "D":
		browser.loading = true
		browser.status = ""
		browser.dsRecordCursor = 0
		return model, tea.Batch(model.loadDSRecords(), spinnerTick())
	case "v":
		vanityServers := fmt.Sprintf("ns1.%s through ns4.%s", browser.currentDomain, browser.currentDomain)
		browser.confirmPrompt = "Delegate " + browser.currentDomain + " to " + vanityServers + "? Vanity name servers must be enabled first."
		browser.confirmCommand = model.delegateToVanity()
		browser.confirmReturn = screenDomainSettings
		browser.screen = screenConfirmAction
	case "V":
		browser.confirmPrompt = "Point " + browser.currentDomain + " back at DNSimple's own name servers?"
		browser.confirmCommand = model.dedelegateFromVanity()
		browser.confirmReturn = screenDomainSettings
		browser.screen = screenConfirmAction
	case "y":
		browser.confirmPrompt = "Enable vanity name servers for " + browser.currentDomain + "? (ns1." + browser.currentDomain + " and friends)"
		browser.confirmCommand = model.setVanity(true)
		browser.confirmReturn = screenDomainSettings
		browser.screen = screenConfirmAction
		return model, nil
	case "Y":
		browser.confirmPrompt = "Disable vanity name servers for " + browser.currentDomain + "?"
		browser.confirmCommand = model.setVanity(false)
		browser.confirmReturn = screenDomainSettings
		browser.screen = screenConfirmAction
		return model, nil
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

func (model sessionModel) textViewKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	limit := len(browser.textLines) - model.visibleRows()
	if limit < 0 {
		limit = 0
	}
	switch key.String() {
	case "q", "esc", "b":
		browser.textLines = nil // a shown secret does not linger in state
		browser.screen = browser.textReturn
		browser.status = ""
	case "up", "k":
		browser.textOffset = max(0, browser.textOffset-1)
	case "down", "j":
		browser.textOffset = min(limit, browser.textOffset+1)
	case "pgup":
		browser.textOffset = max(0, browser.textOffset-model.visibleRows())
	case "pgdown":
		browser.textOffset = min(limit, browser.textOffset+model.visibleRows())
	}
	return model, nil
}

func (model sessionModel) viewTextView() string {
	browser := model.browser
	lines := []string{model.header(browser.textTitle), ""}
	end := min(browser.textOffset+model.visibleRows(), len(browser.textLines))
	lines = append(lines, browser.textLines[browser.textOffset:end]...)
	if len(browser.textLines) > model.visibleRows() {
		lines = append(lines, model.styles.subtle.Render(
			fmt.Sprintf("  %d-%d/%d", browser.textOffset+1, end, len(browser.textLines))))
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("scroll · esc back"))...)
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

func (model sessionModel) contactsKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visibleContacts := model.filteredContacts()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.contactCursor = clamp(browser.contactCursor-1, len(visibleContacts))
	case "down", "j":
		browser.contactCursor = clamp(browser.contactCursor+1, len(visibleContacts))
	case "pgup":
		browser.contactCursor = clamp(browser.contactCursor-model.visibleRows(), len(visibleContacts))
	case "pgdown":
		browser.contactCursor = clamp(browser.contactCursor+model.visibleRows(), len(visibleContacts))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadContacts(), spinnerTick())
	case "n":
		model.openContactForm(nil)
		return model, textinput.Blink
	case "e":
		if len(visibleContacts) == 0 {
			return model, nil
		}
		contact := visibleContacts[browser.contactCursor]
		model.openContactForm(&contact)
		return model, textinput.Blink
	case "d":
		if len(visibleContacts) == 0 {
			return model, nil
		}
		contact := visibleContacts[browser.contactCursor]
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
	visibleTemplates := model.filteredTemplates()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.templateCursor = clamp(browser.templateCursor-1, len(visibleTemplates))
	case "down", "j":
		browser.templateCursor = clamp(browser.templateCursor+1, len(visibleTemplates))
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
	case "e":
		if len(visibleTemplates) == 0 {
			return model, nil
		}
		existing := visibleTemplates[browser.templateCursor]
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
		browser.templateForm[0].SetValue(existing.Name)
		browser.templateForm[1].SetValue(existing.SID)
		browser.templateForm[2].SetValue(existing.Description)
		browser.editingTemplateID = existing.ID
		browser.templateForm[0].Prompt = "> "
		browser.templateForm[0].Focus()
		browser.templateFocused = 0
		browser.editingTemplateID = 0
		browser.screen = screenTemplateForm
		browser.status = ""
		return model, textinput.Blink
	case "d":
		if len(visibleTemplates) == 0 {
			return model, nil
		}
		template := visibleTemplates[browser.templateCursor]
		browser.confirmPrompt = "Delete the template " + template.Name + " and its records?"
		browser.confirmCommand = model.deleteTemplate(template)
		browser.confirmReturn = screenTemplates
		browser.screen = screenConfirmAction
	case "enter":
		if len(visibleTemplates) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		browser.templateRecordCursor = 0
		return model, tea.Batch(
			model.loadTemplateRecords(visibleTemplates[browser.templateCursor]), spinnerTick())
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
	visibleTemplateRecords := model.filteredTemplateRecords()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenTemplates
		browser.status = ""
	case "up", "k":
		browser.templateRecordCursor = clamp(browser.templateRecordCursor-1, len(visibleTemplateRecords))
	case "down", "j":
		browser.templateRecordCursor = clamp(browser.templateRecordCursor+1, len(visibleTemplateRecords))
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
		if len(visibleTemplateRecords) == 0 {
			return model, nil
		}
		record := visibleTemplateRecords[browser.templateRecordCursor]
		browser.confirmPrompt = "Delete the template record " + record.Type + " " + record.Content + "?"
		browser.confirmCommand = model.deleteTemplateRecord(record)
		browser.confirmReturn = screenTemplateRecords
		browser.screen = screenConfirmAction
	}
	return model, nil
}

func (model sessionModel) templatePickerKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visibleTemplates := model.filteredTemplates()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenZones
		browser.status = ""
	case "up", "k":
		browser.templateCursor = clamp(browser.templateCursor-1, len(visibleTemplates))
	case "down", "j":
		browser.templateCursor = clamp(browser.templateCursor+1, len(visibleTemplates))
	case "enter":
		if len(visibleTemplates) == 0 {
			return model, nil
		}
		template := visibleTemplates[browser.templateCursor]
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
	visibleServices := model.filteredServices(false)
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenZones
		browser.status = ""
	case "up", "k":
		browser.serviceCursor = clamp(browser.serviceCursor-1, len(visibleServices))
	case "down", "j":
		browser.serviceCursor = clamp(browser.serviceCursor+1, len(visibleServices))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadServices(false), spinnerTick())
	case "a":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadServices(true), spinnerTick())
	case "d":
		if len(visibleServices) == 0 {
			return model, nil
		}
		service := visibleServices[browser.serviceCursor]
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
	visibleServices := model.filteredServices(true)
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenDomainServices
		browser.status = ""
	case "up", "k":
		browser.servicePickerCursor = clamp(browser.servicePickerCursor-1, len(visibleServices))
	case "down", "j":
		browser.servicePickerCursor = clamp(browser.servicePickerCursor+1, len(visibleServices))
	case "enter":
		if len(visibleServices) == 0 {
			return model, nil
		}
		service := visibleServices[browser.servicePickerCursor]
		browser.confirmPrompt = "Apply the service " + service.Name + " to " + browser.currentDomain + "?"
		browser.confirmCommand = model.applyService(service)
		browser.confirmReturn = screenDomainServices
		browser.screen = screenConfirmAction
	}
	return model, nil
}

func (model sessionModel) pushesKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visiblePushes := model.filteredPushes()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.pushCursor = clamp(browser.pushCursor-1, len(visiblePushes))
	case "down", "j":
		browser.pushCursor = clamp(browser.pushCursor+1, len(visiblePushes))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadPushes(), spinnerTick())
	case "a":
		if len(visiblePushes) == 0 {
			return model, nil
		}
		push := visiblePushes[browser.pushCursor]
		browser.confirmPrompt = fmt.Sprintf("Accept push #%d? The domain joins this account, registered to your first contact.", push.ID)
		browser.confirmCommand = model.acceptPush(push)
		browser.confirmReturn = screenPushes
		browser.screen = screenConfirmAction
	case "x":
		if len(visiblePushes) == 0 {
			return model, nil
		}
		push := visiblePushes[browser.pushCursor]
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

func (model sessionModel) registrantPickerKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visibleContacts := model.filteredContacts()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenDomainSettings
		browser.status = ""
	case "up", "k":
		browser.registrantCursor = clamp(browser.registrantCursor-1, len(visibleContacts))
	case "down", "j":
		browser.registrantCursor = clamp(browser.registrantCursor+1, len(visibleContacts))
	case "enter":
		if len(visibleContacts) == 0 {
			return model, nil
		}
		contact := visibleContacts[browser.registrantCursor]
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
	visibleWebhooks := model.filteredWebhooks()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.webhookCursor = clamp(browser.webhookCursor-1, len(visibleWebhooks))
	case "down", "j":
		browser.webhookCursor = clamp(browser.webhookCursor+1, len(visibleWebhooks))
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
		if len(visibleWebhooks) == 0 {
			return model, nil
		}
		webhook := visibleWebhooks[browser.webhookCursor]
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

func (model sessionModel) secondaryDNSKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visiblePrimaries := model.filteredPrimaries()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.primaryCursor = clamp(browser.primaryCursor-1, len(visiblePrimaries))
	case "down", "j":
		browser.primaryCursor = clamp(browser.primaryCursor+1, len(visiblePrimaries))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadSecondaryDNS(), spinnerTick())
	case "n":
		placeholders := [3]string{"name (e.g. ns1.primary.net)", "IP address", "port (53)"}
		for field := range 3 {
			input := textinput.New()
			input.Placeholder = placeholders[field]
			input.CharLimit = 253
			input.Width = 40
			input.Prompt = "  "
			input.PromptStyle = model.styles.selected
			browser.primaryForm[field] = input
		}
		browser.primaryForm[2].SetValue("53")
		browser.primaryForm[0].Prompt = "> "
		browser.primaryForm[0].Focus()
		browser.primaryFocused = 0
		browser.screen = screenPrimaryForm
		browser.status = ""
		return model, textinput.Blink
	case "l":
		if len(visiblePrimaries) == 0 {
			return model, nil
		}
		primary := visiblePrimaries[browser.primaryCursor]
		if primary.Linked {
			browser.confirmPrompt = "Unlink " + primary.Name + "? DNSimple stops slaving its zones."
			browser.confirmCommand = model.setPrimaryLink(primary, false)
			browser.confirmReturn = screenSecondaryDNS
			browser.screen = screenConfirmAction
			return model, nil
		}
		browser.loading = true
		return model, tea.Batch(model.setPrimaryLink(primary, true), spinnerTick())
	case "z":
		input := textinput.New()
		input.Placeholder = "zone to slave from the linked primaries"
		input.CharLimit = 253
		input.Width = 44
		input.Prompt = "> "
		input.PromptStyle = model.styles.selected
		input.Focus()
		browser.secondaryZoneInput = input
		browser.screen = screenSecondaryZoneForm
		browser.status = ""
		return model, textinput.Blink
	case "d":
		if len(visiblePrimaries) == 0 {
			return model, nil
		}
		primary := visiblePrimaries[browser.primaryCursor]
		browser.confirmPrompt = "Delete the primary server " + primary.Name + "?"
		browser.confirmCommand = model.deletePrimary(primary)
		browser.confirmReturn = screenSecondaryDNS
		browser.screen = screenConfirmAction
	}
	return model, nil
}

func (model sessionModel) primaryFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenSecondaryDNS
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.submitPrimary(), spinnerTick())
	case "tab", "down":
		browser.primaryFocused = (browser.primaryFocused + 1) % 3
	case "shift+tab", "up":
		browser.primaryFocused = (browser.primaryFocused + 2) % 3
	default:
		var inputCommand tea.Cmd
		browser.primaryForm[browser.primaryFocused], inputCommand =
			browser.primaryForm[browser.primaryFocused].Update(key)
		return model, inputCommand
	}
	for field := range 3 {
		if field == browser.primaryFocused {
			browser.primaryForm[field].Prompt = "> "
			browser.primaryForm[field].Focus()
		} else {
			browser.primaryForm[field].Prompt = "  "
			browser.primaryForm[field].Blur()
		}
	}
	return model, nil
}

func (model sessionModel) secondaryZoneFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenSecondaryDNS
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.submitSecondaryZone(), spinnerTick())
	}
	var inputCommand tea.Cmd
	browser.secondaryZoneInput, inputCommand = browser.secondaryZoneInput.Update(key)
	return model, inputCommand
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
func fuzzyMatch(query, target string) bool {
	queryRunes := []rune(strings.ToLower(query))
	position := 0
	for _, char := range strings.ToLower(target) {
		if position < len(queryRunes) && char == queryRunes[position] {
			position++
		}
	}
	return position == len(queryRunes)
}

// filteredTLDs is the catalog through the live filter; an empty filter is
// the whole catalog.
func (model sessionModel) filteredTLDs() []insights.TLD {
	query := model.filterQuery()
	if query == "" {
		return model.browser.tldList
	}
	var matched []insights.TLD
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

var konamiCode = [...]string{"up", "up", "down", "down", "left", "right", "left", "right", "b", "a"}

func (model sessionModel) homeKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	// The Konami code toggles rainbow mode; the buffer only ever tracks
	// the last ten keys pressed on the home screen, so the code works as a
	// suffix of whatever came before.
	browser.cheatBuffer = append(browser.cheatBuffer, key.String())
	if len(browser.cheatBuffer) > len(konamiCode) {
		browser.cheatBuffer = browser.cheatBuffer[len(browser.cheatBuffer)-len(konamiCode):]
	}
	if key.String() == "w" {
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadLinkedAccounts(), spinnerTick())
	}
	if slices.Equal(browser.cheatBuffer, konamiCode[:]) {
		browser.partyMode = !browser.partyMode
		browser.cheatBuffer = nil
		if browser.partyMode {
			browser.status = "✓ Konami code - party mode on"
		} else {
			browser.status = "party mode off"
		}
		return model, nil
	}
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
		case "Secondary DNS":
			browser.screen = screenSecondaryDNS
			return model, tea.Batch(model.loadSecondaryDNS(), spinnerTick())
		case "Analytics":
			browser.screen = screenAnalytics
			return model, tea.Batch(model.loadAnalytics(), spinnerTick())
		case "TLDs":
			browser.screen = screenTLDs
			return model, tea.Batch(model.loadTLDs(), spinnerTick())
		default:
			browser.screen = screenBilling
			return model, tea.Batch(model.loadCharges(), spinnerTick())
		}
	}
	return model, nil
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

func (model sessionModel) transferFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenDomains
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		domainName := strings.ToLower(strings.TrimSpace(browser.transferForm[0].Value()))
		return model, tea.Batch(model.checkRegistryDemands("transfer", domainName), spinnerTick())
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

func (model sessionModel) zonesKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visibleZones := model.filteredZones()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.zoneCursor = clamp(browser.zoneCursor-1, len(visibleZones))
	case "down", "j":
		browser.zoneCursor = clamp(browser.zoneCursor+1, len(visibleZones))
	case "pgup":
		browser.zoneCursor = clamp(browser.zoneCursor-model.visibleRows(), len(visibleZones))
	case "pgdown":
		browser.zoneCursor = clamp(browser.zoneCursor+model.visibleRows(), len(visibleZones))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadZones(), spinnerTick())
	case "enter":
		if len(visibleZones) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		browser.recordCursor = 0
		return model, tea.Batch(model.loadRecords(visibleZones[browser.zoneCursor].Name), spinnerTick())
	case "p":
		if len(visibleZones) == 0 {
			return model, nil
		}
		browser.currentDomain = visibleZones[browser.zoneCursor].Name
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadTemplates(true), spinnerTick())
	case "x":
		if len(visibleZones) == 0 {
			return model, nil
		}
		zone := visibleZones[browser.zoneCursor]
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
		if len(visibleZones) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(
			model.checkZoneDistribution(visibleZones[browser.zoneCursor].Name), spinnerTick())
	case "f":
		if len(visibleZones) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadZoneFile(visibleZones[browser.zoneCursor].Name), spinnerTick())
	case "v":
		if len(visibleZones) == 0 {
			return model, nil
		}
		browser.currentDomain = visibleZones[browser.zoneCursor].Name
		browser.loading = true
		browser.status = ""
		browser.serviceCursor = 0
		return model, tea.Batch(model.loadServices(false), spinnerTick())
	}
	return model, nil
}

func (model sessionModel) recordsKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visibleRecords := model.filteredRecords()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenZones
		browser.status = ""
	case "up", "k":
		browser.recordCursor = clamp(browser.recordCursor-1, len(visibleRecords))
	case "down", "j":
		browser.recordCursor = clamp(browser.recordCursor+1, len(visibleRecords))
	case "pgup":
		browser.recordCursor = clamp(browser.recordCursor-model.visibleRows(), len(visibleRecords))
	case "pgdown":
		browser.recordCursor = clamp(browser.recordCursor+model.visibleRows(), len(visibleRecords))
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
		if len(visibleRecords) == 0 {
			return model, nil
		}
		record := visibleRecords[browser.recordCursor]
		browser.form = newRecordForm(&record, model.styles)
		browser.formForTemplate = false
		browser.screen = screenForm
		browser.status = ""
		return model, textinput.Blink
	case "D":
		if len(visibleRecords) == 0 {
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.checkRecordDistribution(visibleRecords[browser.recordCursor]), spinnerTick())
	case "N":
		var current []string
		for _, record := range browser.records {
			if record.Type == "NS" && record.Name == "" {
				current = append(current, record.Content)
			}
		}
		input := textinput.New()
		input.Placeholder = "ns1.dnsimple.com, ns2.dnsimple.com, ..."
		input.CharLimit = 500
		input.Width = 60
		input.SetValue(strings.Join(current, ", "))
		input.Focus()
		browser.registerInput = input
		browser.screen = screenNSRecordsForm
		browser.status = ""
		return model, textinput.Blink
	case "d":
		if len(visibleRecords) == 0 {
			return model, nil
		}
		record := visibleRecords[browser.recordCursor]
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
	case screenDSRecords:
		body = model.viewDSRecords()
	case screenDSRecordForm:
		body = model.viewDSRecordForm()
	case screenRegistrantChanges:
		body = model.viewRegistrantChanges()
	case screenAddDomainForm:
		body = model.viewAddDomainForm()
	case screenNSRecordsForm:
		body = model.viewNSRecordsForm()
	case screenExtAttrForm:
		body = model.viewExtAttrForm()
	case screenAccountPicker:
		body = model.viewAccountPicker()
	case screenSecondaryDNS:
		body = model.viewSecondaryDNS()
	case screenPrimaryForm:
		body = model.viewPrimaryForm()
	case screenSecondaryZoneForm:
		body = model.viewSecondaryZoneForm()
	case screenAnalytics:
		body = model.viewAnalytics()
	case screenTLDs:
		body = model.viewTLDs()
	case screenTextView:
		body = model.viewTextView()
	case screenConfirmAction:
		body = model.viewConfirmAction()
	}
	return model.framed(body)
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
		line := fmt.Sprintf("%-40.40s -> %s", forward.From, forward.To)
		cursor := "    "
		if index == browser.forwardCursor {
			cursor = "  > "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
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
	contactsListed := model.filteredContacts()
	if len(contactsListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no contacts to show"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.contactCursor, len(contactsListed), visible)
	for index := start; index < end; index++ {
		contact := contactsListed[index]
		line := fmt.Sprintf("%-46.46s %s, %s", contact.Label(), contact.City, contact.Country)
		cursor := "  "
		if index == browser.contactCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.contactCursor, len(contactsListed), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
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
	if picker {
		title = "apply a template to " + browser.currentDomain
	}
	lines := []string{model.header(title), ""}
	templatesListed := model.filteredTemplates()
	if len(templatesListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no templates to show"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.templateCursor, len(templatesListed), visible)
	for index := start; index < end; index++ {
		template := templatesListed[index]
		line := fmt.Sprintf("%-28.28s %-20.20s %s", template.Name, template.SID, template.Description)
		cursor := "  "
		if index == browser.templateCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.templateCursor, len(templatesListed), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left, append(lines, model.actionFooter())...)
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
	templateRecordsListed := model.filteredTemplateRecords()
	if len(templateRecordsListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no records to show"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.templateRecordCursor, len(templateRecordsListed), visible)
	for index := start; index < end; index++ {
		record := templateRecordsListed[index]
		name := record.Name
		if name == "" {
			name = "@"
		}
		line := fmt.Sprintf("%-20.20s %-6s %-35.35s %6d", name, record.Type, record.Content, record.TTL)
		cursor := "  "
		if index == browser.templateRecordCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.templateRecordCursor, len(templateRecordsListed), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
}

func (model sessionModel) viewDomainServices(picker bool) string {
	browser := model.browser
	title := browser.currentDomain + " · services"
	listed := browser.serviceList
	cursor := browser.serviceCursor
	empty := "  none applied; a adds one"
	if picker {
		title = "apply a service to " + browser.currentDomain
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
		line := fmt.Sprintf("%-28.28s %s", service.Name, service.Description)
		prefix := "  "
		if index == cursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left, append(lines, model.actionFooter())...)
}

func (model sessionModel) viewPushes() string {
	browser := model.browser
	lines := []string{model.header("incoming pushes"), ""}
	pushesListed := model.filteredPushes()
	if len(pushesListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no pushes to show"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.pushCursor, len(pushesListed), visible)
	for index := start; index < end; index++ {
		push := pushesListed[index]
		line := fmt.Sprintf("push #%-6d domain id %-8d since %s", push.ID, push.DomainID, humanTime(push.CreatedAt))
		prefix := "  "
		if index == browser.pushCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
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

func (model sessionModel) viewRegistrantPicker() string {
	browser := model.browser
	lines := []string{model.header("new registrant for " + browser.currentDomain), ""}
	if len(browser.contactList) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no contacts; create one in the Contacts screen first"))
	}
	visible := model.visibleRows()
	registrantContacts := model.filteredContacts()
	start, end := listWindow(browser.registrantCursor, len(registrantContacts), visible)
	for index := start; index < end; index++ {
		line := registrantContacts[index].Label()
		prefix := "  "
		if index == browser.registrantCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
}

func (model sessionModel) viewWebhooks() string {
	browser := model.browser
	lines := []string{model.header("webhooks"), ""}
	webhooksListed := model.filteredWebhooks()
	if len(webhooksListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no webhooks to show"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.webhookCursor, len(webhooksListed), visible)
	for index := start; index < end; index++ {
		line := webhooksListed[index].URL
		prefix := "  "
		if index == browser.webhookCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
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

func (model sessionModel) viewSecondaryDNS() string {
	browser := model.browser
	lines := []string{model.header("secondary dns"), ""}
	primariesListed := model.filteredPrimaries()
	if len(primariesListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no primary servers to show"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.primaryCursor, len(primariesListed), visible)
	for index := start; index < end; index++ {
		primary := primariesListed[index]
		linkState := "unlinked"
		if primary.Linked {
			linkState = "linked"
		}
		line := fmt.Sprintf("%-36.36s %s:%d  %s", primary.Name, primary.IP, primary.Port, linkState)
		prefix := "  "
		if index == browser.primaryCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	if len(browser.secondaryZoneNames) > 0 {
		lines = append(lines, "",
			model.styles.subtle.Render("  secondary zones: "+strings.Join(browser.secondaryZoneNames, ", ")))
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
}

func (model sessionModel) viewPrimaryForm() string {
	browser := model.browser
	labels := [3]string{"Name ", "IP   ", "Port "}
	lines := []string{model.header("new primary server"), ""}
	for field := range 3 {
		lines = append(lines, "  "+labels[field]+" "+browser.primaryForm[field].View())
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("tab next field · enter create · esc cancel"))...)
}

func (model sessionModel) viewSecondaryZoneForm() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		model.header("new secondary zone"),
		"",
		"  Zone  "+model.browser.secondaryZoneInput.View(),
		"",
		model.styles.subtle.Render("  DNSimple slaves it from the linked primaries"),
		model.footer("enter create · esc cancel"),
	)
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

func (model sessionModel) viewHome() string {
	lines := []string{model.header("home"), ""}
	for index, entry := range homeEntries {
		cursor := "  "
		if index == model.browser.homeCursor {
			cursor = "> "
			entry = model.styles.selected.Render(entry)
		} else if model.browser.partyMode {
			// The horizontal join with the artwork resets the per-line
			// tint before it reaches the menu, so the entries take their
			// party colors directly.
			entry = model.styles.renderer.NewStyle().Foreground(partyColor()).Render(entry)
		}
		lines = append(lines, cursor+entry)
	}
	menu := lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
	return lipgloss.JoinHorizontal(lipgloss.Center, model.artwork, "   ", menu)
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
	line := title + account
	// Errors and outcomes live at the top, where the eye already is.
	if model.browser.status != "" {
		line += "\n" + model.styles.status.Render(model.browser.status)
	}
	if model.browser.listFilterOn && filterableScreens[model.browser.screen] {
		line += "\n" + model.browser.listFilter.View()
	}
	return line
}

func (model sessionModel) footer(hints string) string {
	lines := []string{""}
	if model.browser.loading {
		lines = append(lines, spinnerFrames[model.spinnerFrame]+" talking to DNSimple...")
	}
	lines = append(lines, model.styles.subtle.Render(hints))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (model sessionModel) viewZones() string {
	browser := model.browser
	lines := []string{model.header("zones"), ""}
	zonesListed := model.filteredZones()
	if len(zonesListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no zones to show"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.zoneCursor, len(zonesListed), visible)
	for index := start; index < end; index++ {
		cursor := "  "
		zone := zonesListed[index]
		state := "active"
		if !zone.Active {
			state = "inactive"
		}
		line := fmt.Sprintf("%-61.61s %s", zone.Name, state)
		if index == browser.zoneCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.zoneCursor, len(zonesListed), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
}

func (model sessionModel) viewRecords() string {
	browser := model.browser
	lines := []string{model.header(browser.currentZone), ""}
	recordsListed := model.filteredRecords()
	if len(recordsListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  no records to show"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.recordCursor, len(recordsListed), visible)
	for index := start; index < end; index++ {
		record := recordsListed[index]
		name := record.Name
		if name == "" {
			name = "@"
		}
		line := fmt.Sprintf("%-20.20s %-6s %-35.35s %6d", name, record.Type, record.Content, record.TTL)
		cursor := "  "
		if index == browser.recordCursor {
			cursor = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	if position := model.positionLine(browser.recordCursor, len(recordsListed), visible); position != "" {
		lines = append(lines, position)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
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

// visibleRows is how many list rows fit between the header and the footer
// of the fixed-height frame - lists scroll inside it, they never stretch it.
func (model sessionModel) visibleRows() int {
	// header (up to 2 with a status), blank, position line, footer blank,
	// loading line, hints: 7 rows fits the worst case, so an overflowing
	// list reaches the footer instead of leaving a band of dots.
	chrome := 7
	if model.browser.listFilterOn {
		chrome++
	}
	if rows := model.frameHeight() - chrome; rows >= 4 {
		return rows
	}
	return 4
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

// humanTime renders API timestamps the way people read them: date-only
// values as "Jul 24, 2026", instants as "Jul 24, 2026 - 10:02 am".
// Anything unparseable passes through untouched.
func humanTime(raw string) string {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, raw)
		if err != nil {
			continue
		}
		if parsed.Hour() == 0 && parsed.Minute() == 0 {
			return parsed.Format("Jan 2, 2006")
		}
		return parsed.Format("Jan 2, 2006 - 3:04 pm")
	}
	return raw
}

// filteredAllCerts is the portfolio-wide certificate list through the live
// filter, matching domain and common name.
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

func (model sessionModel) filteredDSRecords() []domains.DSRecord {
	return filterItems(model, model.browser.dsRecordList, func(record domains.DSRecord) string {
		return record.KeyTag + " " + record.Algorithm + " " + record.Digest
	})
}

type dsRecordsLoadedMessage struct {
	records []domains.DSRecord
	err     error
}

type dsRecordMutatedMessage struct {
	verb string
	err  error
}

type recordDistributionMessage struct {
	label       string
	distributed bool
	err         error
}

func (model sessionModel) loadDSRecords() tea.Cmd {
	component, session, ctx := model.components.Domains, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.DSRecords(ctx, session, domainName)
		if outcome.Failed() {
			return dsRecordsLoadedMessage{err: outcome.Error()}
		}
		return dsRecordsLoadedMessage{records: outcome.Value()}
	}
}

func (model sessionModel) submitDSRecord() tea.Cmd {
	component, session, ctx := model.components.Domains, model.session, model.appCtx
	domainName := model.browser.currentDomain
	form := model.browser.dsForm
	record := domains.DSRecord{
		KeyTag:     strings.TrimSpace(form[0].Value()),
		Algorithm:  strings.TrimSpace(form[1].Value()),
		DigestType: strings.TrimSpace(form[2].Value()),
		Digest:     strings.TrimSpace(form[3].Value()),
	}
	return func() tea.Msg {
		outcome := component.CreateDSRecord(ctx, session, domainName, record)
		return dsRecordMutatedMessage{verb: "create", err: outcome.Error()}
	}
}

func (model sessionModel) deleteDSRecord(record domains.DSRecord) tea.Cmd {
	component, session, ctx := model.components.Domains, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.DeleteDSRecord(ctx, session, domainName, record.ID)
		return dsRecordMutatedMessage{verb: "delete", err: outcome.Error()}
	}
}

func (model sessionModel) checkRecordDistribution(record zones.Record) tea.Cmd {
	component, session, ctx := model.components.Zones, model.session, model.appCtx
	zoneName := model.browser.currentZone
	label := record.Type + " " + record.Content
	return func() tea.Msg {
		outcome := component.RecordDistribution(ctx, session, zoneName, record.ID)
		if outcome.Failed() {
			return recordDistributionMessage{label: label, err: outcome.Error()}
		}
		return recordDistributionMessage{label: label, distributed: outcome.Value()}
	}
}

func (model sessionModel) setVanity(enabled bool) tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.SetVanityNameServers(ctx, session, domainName, enabled)
		return settingChangedMessage{what: "vanity name servers", err: outcome.Error()}
	}
}

func (model sessionModel) dsRecordsKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	visibleDS := model.filteredDSRecords()
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenDomainSettings
		browser.status = ""
	case "up", "k":
		browser.dsRecordCursor = clamp(browser.dsRecordCursor-1, len(visibleDS))
	case "down", "j":
		browser.dsRecordCursor = clamp(browser.dsRecordCursor+1, len(visibleDS))
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadDSRecords(), spinnerTick())
	case "n":
		placeholders := [4]string{"key tag", "algorithm (e.g. 13)", "digest type (e.g. 2)", "digest (hex)"}
		for field := range 4 {
			input := textinput.New()
			input.Placeholder = placeholders[field]
			input.CharLimit = 253
			input.Width = 44
			input.Prompt = "  "
			input.PromptStyle = model.styles.selected
			browser.dsForm[field] = input
		}
		browser.dsForm[0].Prompt = "> "
		browser.dsForm[0].Focus()
		browser.dsFocused = 0
		browser.screen = screenDSRecordForm
		browser.status = ""
		return model, textinput.Blink
	case "d":
		if len(visibleDS) == 0 {
			return model, nil
		}
		record := visibleDS[browser.dsRecordCursor]
		browser.confirmPrompt = "Delete DS record #" + strconv.FormatInt(record.ID, 10) +
			"? Validating resolvers stop trusting the matching key."
		browser.confirmCommand = model.deleteDSRecord(record)
		browser.confirmReturn = screenDSRecords
		browser.screen = screenConfirmAction
	}
	return model, nil
}

func (model sessionModel) dsRecordFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenDSRecords
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.submitDSRecord(), spinnerTick())
	case "tab", "down":
		browser.dsFocused = (browser.dsFocused + 1) % 4
	case "shift+tab", "up":
		browser.dsFocused = (browser.dsFocused + 3) % 4
	default:
		var inputCommand tea.Cmd
		browser.dsForm[browser.dsFocused], inputCommand = browser.dsForm[browser.dsFocused].Update(key)
		return model, inputCommand
	}
	for field := range 4 {
		if field == browser.dsFocused {
			browser.dsForm[field].Prompt = "> "
			browser.dsForm[field].Focus()
		} else {
			browser.dsForm[field].Prompt = "  "
			browser.dsForm[field].Blur()
		}
	}
	return model, nil
}

func (model sessionModel) viewDSRecords() string {
	browser := model.browser
	lines := []string{model.header(browser.currentDomain + " · ds records"), ""}
	dsListed := model.filteredDSRecords()
	if len(dsListed) == 0 && !browser.loading {
		lines = append(lines, model.styles.subtle.Render("  none; n publishes one to the parent zone"))
	}
	visible := model.visibleRows()
	start, end := listWindow(browser.dsRecordCursor, len(dsListed), visible)
	for index := start; index < end; index++ {
		record := dsListed[index]
		line := fmt.Sprintf("tag %-7s alg %-4s type %-3s %.40s", record.KeyTag, record.Algorithm, record.DigestType, record.Digest)
		prefix := "  "
		if index == browser.dsRecordCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
}

// changeRow flattens registrant changes and this session's transfers into
// one cancellable list; kind dispatches the cancel call.
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

type changeCancelledMessage struct {
	kind string
	id   int64
	err  error
}

type linkedAccountsMessage struct {
	tenants []identity.Tenant
	err     error
}

type accountSwitchedMessage struct {
	session identity.Session
	err     error
}

type relinkStartedMessage struct {
	pending identity.PendingLink
	err     error
}

func (model sessionModel) loadLinkedAccounts() tea.Cmd {
	component, fingerprint, ctx := model.components.Identity, model.fingerprint, model.appCtx
	return func() tea.Msg {
		outcome := component.LinkedTenants(ctx, fingerprint)
		if outcome.Failed() {
			return linkedAccountsMessage{err: outcome.Error()}
		}
		return linkedAccountsMessage{tenants: outcome.Value()}
	}
}

func (model sessionModel) switchAccount(tenantID int64) tea.Cmd {
	component, fingerprint, ctx := model.components.Identity, model.fingerprint, model.appCtx
	return func() tea.Msg {
		outcome := component.SwitchAccount(ctx, fingerprint, tenantID)
		return accountSwitchedMessage{session: outcome.Value(), err: outcome.Error()}
	}
}

func (model sessionModel) beginRelink() tea.Cmd {
	component, fingerprint, ctx := model.components.Identity, model.fingerprint, model.appCtx
	return func() tea.Msg {
		outcome := component.BeginLink(ctx, fingerprint)
		return relinkStartedMessage{pending: outcome.Value(), err: outcome.Error()}
	}
}

func (model sessionModel) accountPickerKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "esc", "b":
		browser.screen = screenHome
		browser.status = ""
	case "up", "k":
		browser.accountCursor = clamp(browser.accountCursor-1, len(browser.accountList))
	case "down", "j":
		browser.accountCursor = clamp(browser.accountCursor+1, len(browser.accountList))
	case "enter":
		if len(browser.accountList) == 0 {
			return model, nil
		}
		chosen := browser.accountList[browser.accountCursor]
		if chosen.ID == model.session.Tenant.ID {
			browser.status = "already on account #" + strconv.FormatInt(chosen.AccountID, 10)
			return model, nil
		}
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.switchAccount(chosen.ID), spinnerTick())
	case "n":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.beginRelink(), spinnerTick())
	}
	return model, nil
}

func (model sessionModel) viewAccountPicker() string {
	browser := model.browser
	lines := []string{model.header("accounts"), ""}
	for index, tenant := range browser.accountList {
		line := fmt.Sprintf("#%-8d %s", tenant.AccountID, tenant.Email)
		if tenant.ID == model.session.Tenant.ID {
			line += "  (current)"
		}
		prefix := "  "
		if index == browser.accountCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.actionFooter())...)
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

type nsRecordsUpdatedMessage struct {
	err error
}

func (model sessionModel) updateNSRecords() tea.Cmd {
	component, session, ctx := model.components.Zones, model.session, model.appCtx
	zoneName := model.browser.currentZone
	nsNames := strings.Split(model.browser.registerInput.Value(), ",")
	return func() tea.Msg {
		outcome := component.UpdateNSRecords(ctx, session, zoneName, nsNames)
		return nsRecordsUpdatedMessage{err: outcome.Error()}
	}
}

func (model sessionModel) nsRecordsFormKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "esc":
		browser.screen = screenRecords
		browser.status = ""
		return model, nil
	case "enter":
		browser.loading = true
		return model, tea.Batch(model.updateNSRecords(), spinnerTick())
	}
	var inputCommand tea.Cmd
	browser.registerInput, inputCommand = browser.registerInput.Update(key)
	return model, inputCommand
}

func (model sessionModel) viewNSRecordsForm() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		model.header(model.browser.currentZone+" · ns records"),
		"",
		"  Servers  "+model.browser.registerInput.View(),
		"",
		model.styles.subtle.Render("  comma-separated; replaces the zone's whole NS set in one call"),
		model.footer("enter update · esc cancel"),
	)
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

func (model sessionModel) viewDSRecordForm() string {
	browser := model.browser
	labels := [4]string{"Key tag    ", "Algorithm  ", "Digest type", "Digest     "}
	lines := []string{model.header(browser.currentDomain + " · new ds record"), ""}
	for field := range 4 {
		lines = append(lines, "  "+labels[field]+" "+browser.dsForm[field].View())
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("tab next field · enter publish · esc cancel"))...)
}
