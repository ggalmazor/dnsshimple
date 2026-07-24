package sshui

import (
	"errors"
	"fmt"
	"slices"
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
	tldList            []registrar.TLD
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

	extAttrDefs    []registrar.ExtendedAttribute
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

type mutationDoneMessage struct {
	verb string
	err  error
}

type registrationSubmittedMessage struct {
	domainName string
	err        error
}

type settingChangedMessage struct {
	what string
	err  error
}

type pendingTransfer struct {
	domain   string
	transfer registrar.Transfer
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

func (model sessionModel) setTransferLock(enabled bool) tea.Cmd {
	component, session, ctx := model.components.Registrar, model.session, model.appCtx
	domainName := model.browser.currentDomain
	return func() tea.Msg {
		outcome := component.SetTransferLock(ctx, session, domainName, enabled)
		return settingChangedMessage{what: "transfer lock", err: outcome.Error()}
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

type textLoadedMessage struct {
	title  string
	text   string
	back   appScreen
	err    error
	secret bool // secrets show with a reminder, never in the status line
}

type action struct {
	key   string
	label string
}

// screenActions drives the tab-navigable footers. Form screens are absent
// on purpose: they own tab for field navigation.
var screenActions = map[appScreen][]action{
	screenHome:              {{"enter", "open"}, {"w", "accounts"}, {"q", "quit"}},
	screenAccountPicker:     {{"enter", "switch"}, {"n", "link another"}, {"x", "unlink"}, {"esc", "back"}},
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

func (model sessionModel) filteredPrimaries() []secondarydns.PrimaryServer {
	return filterItems(model, model.browser.primaryList, func(primary secondarydns.PrimaryServer) string {
		return primary.Name + " " + primary.IP
	})
}

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
		browser.confirmCommand = model.submitRegistration(nil)
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
			var needs registrar.NeedsExtendedAttributesError
			if errors.As(message.err, &needs) {
				return model.openDemandsForm("register", needs.Demands)
			}
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

	case accountUnlinkedMessage:
		browser.loading = false
		if message.err != nil {
			browser.status = statusFor(message.err)
			browser.screen = screenAccountPicker
			return model, nil
		}
		browser.status = "✓ account unlinked"
		browser.loading = true
		browser.screen = screenAccountPicker
		return model, tea.Batch(model.loadLinkedAccounts(), spinnerTick())

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
		model.browser.status = "✓ switched to " + accountLabel(message.session.Tenant)
		model.session = message.session
		return model, nil

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
			var needs registrar.NeedsExtendedAttributesError
			if errors.As(message.err, &needs) {
				return model.openDemandsForm("transfer", needs.Demands)
			}
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

func (model sessionModel) viewConfirmAction() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		model.header(model.browser.currentDomain),
		"",
		model.styles.errorText.Render(model.browser.confirmPrompt),
		model.footer("y confirm · n cancel"),
	)
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

func autoRenewLabel(autoRenew bool) string {
	if autoRenew {
		return "auto-renew"
	}
	return "manual"
}

func (model sessionModel) header(crumb string) string {
	title := model.styles.title.Render("DNSSHIMPLE")
	account := model.styles.subtle.Render(" " + crumb + " · " + accountLabel(model.session.Tenant))
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
type changeCancelledMessage struct {
	kind string
	id   int64
	err  error
}

type accountSwitchedMessage struct {
	session identity.Session
	err     error
}
