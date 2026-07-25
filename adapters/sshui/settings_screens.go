package sshui

import (
	"dnsshimple/core/domains"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type settingsLoadedMessage struct {
	domainName string
	dnssec     bool
	whois      bool
	lock       bool
	delegation []string
	forwards   []domains.EmailForward
	partial    error
	err        error
}

func (model sessionModel) loadSettings(domainName string) tea.Cmd {
	component, session, ctx := model.components.Domains, model.session, model.appCtx
	return func() tea.Msg {
		settings := component.InspectDomain(ctx, session, domainName)
		message := settingsLoadedMessage{
			domainName: domainName,
			dnssec:     settings.DNSSECEnabled,
			whois:      settings.WhoisPrivacy,
			lock:       settings.TransferLock,
			delegation: settings.Delegation,
			forwards:   settings.EmailForwards,
		}
		if len(settings.Partial) > 0 {
			// Degraded, not dead: the screen renders what loaded and
			// names the first failure.
			message.partial = settings.Partial[0]
		}
		return message
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

func dsRecordSearchText(record domains.DSRecord) string {
	return record.KeyTag + " " + record.Algorithm + " " + record.Digest
}

func (model sessionModel) filteredDSRecords() []domains.DSRecord {
	return filterItems(model, model.browser.dsRecordList, dsRecordSearchText)
}

type dsRecordsLoadedMessage struct {
	records []domains.DSRecord
	err     error
}

type dsRecordMutatedMessage struct {
	verb string
	err  error
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
