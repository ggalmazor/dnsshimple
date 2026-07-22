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

	"dnsshimple/core/gateway"
	"dnsshimple/core/zones"
)

// The zones browser: list zones, list records, edit in a form, confirm
// deletes. Every operation goes through the zones Component, so validation,
// rate limiting, caching, and the breaker all apply before DNSimple is hit.

type appScreen int

const (
	screenZones appScreen = iota
	screenRecords
	screenForm
	screenConfirmDelete
)

type browserState struct {
	screen       appScreen
	loading      bool
	status       string
	zoneList     []zones.Zone
	zoneCursor   int
	currentZone  string
	records      []zones.Record
	recordCursor int
	form         recordForm
	deleteTarget *zones.Record
}

func newBrowserState() browserState {
	return browserState{screen: screenZones, loading: true}
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

func (model sessionModel) loadZones() tea.Cmd {
	component, session, ctx := model.zones, model.session, model.appCtx
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
	component, session, ctx := model.zones, model.session, model.appCtx
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
	component, session, ctx := model.zones, model.session, model.appCtx
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
	component, session, ctx := model.zones, model.session, model.appCtx
	zoneName := model.browser.currentZone
	return func() tea.Msg {
		outcome := component.DeleteRecord(ctx, session, zoneName, record.ID)
		return mutationDoneMessage{verb: "delete", err: outcome.Error()}
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

	case tea.KeyMsg:
		switch browser.screen {
		case screenZones:
			return model.zonesKeys(message)
		case screenRecords:
			return model.recordsKeys(message)
		case screenForm:
			return model.formKeys(message)
		case screenConfirmDelete:
			return model.confirmKeys(message)
		}
	}
	return model, nil
}

func (model sessionModel) zonesKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	browser := &model.browser
	switch key.String() {
	case "q":
		return model, tea.Quit
	case "up", "k":
		browser.zoneCursor = clamp(browser.zoneCursor-1, len(browser.zoneList))
	case "down", "j":
		browser.zoneCursor = clamp(browser.zoneCursor+1, len(browser.zoneList))
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
	case "r":
		browser.loading = true
		browser.status = ""
		return model, tea.Batch(model.loadRecords(browser.currentZone), spinnerTick())
	case "n":
		browser.form = newRecordForm(nil)
		browser.screen = screenForm
		browser.status = ""
		return model, textinput.Blink
	case "e":
		if len(browser.records) == 0 {
			return model, nil
		}
		record := browser.records[browser.recordCursor]
		browser.form = newRecordForm(&record)
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

func newRecordForm(existing *zones.Record) recordForm {
	form := recordForm{}
	labels := [fieldCount]string{"name (empty = apex)", "type (A, CNAME, MX, ...)", "content", "ttl", "priority"}
	for field := range fieldCount {
		input := textinput.New()
		input.Placeholder = labels[field]
		input.CharLimit = 253
		input.Width = 40
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
			form.inputs[index].Focus()
		} else {
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
	case screenZones:
		body = model.viewZones()
	case screenRecords:
		body = model.viewRecords()
	case screenForm:
		body = model.viewForm()
	case screenConfirmDelete:
		body = model.viewConfirm()
	}
	return frameStyle.Render(body) + "\n"
}

func (model sessionModel) header(crumb string) string {
	title := titleStyle.Render("DNSSHIMPLE")
	account := subtleStyle.Render(fmt.Sprintf(" %s · account #%d", crumb, model.session.Tenant.AccountID))
	return title + account
}

func (model sessionModel) footer(hints string) string {
	lines := []string{""}
	if model.browser.loading {
		lines = append(lines, spinnerFrames[model.spinnerFrame]+" talking to DNSimple...")
	}
	if model.browser.status != "" {
		lines = append(lines, statusStyle.Render(model.browser.status))
	}
	lines = append(lines, subtleStyle.Render(hints))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (model sessionModel) viewZones() string {
	browser := model.browser
	lines := []string{model.header("zones"), ""}
	if len(browser.zoneList) == 0 && !browser.loading {
		lines = append(lines, subtleStyle.Render("  no zones in this account"))
	}
	for index, zone := range browser.zoneList {
		cursor := "  "
		line := zone.Name
		if index == browser.zoneCursor {
			cursor = "> "
			line = selectedStyle.Render(line)
		}
		lines = append(lines, cursor+line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		append(lines, model.footer("enter open · r refresh · q quit"))...)
}

func (model sessionModel) viewRecords() string {
	browser := model.browser
	lines := []string{model.header(browser.currentZone), ""}
	if len(browser.records) == 0 && !browser.loading {
		lines = append(lines, subtleStyle.Render("  no records; n creates the first one"))
	}
	for index, record := range browser.records {
		name := record.Name
		if name == "" {
			name = "@"
		}
		line := fmt.Sprintf("%-20.20s %-6s %-32.32s %6d", name, record.Type, record.Content, record.TTL)
		cursor := "  "
		if index == browser.recordCursor {
			cursor = "> "
			line = selectedStyle.Render(line)
		}
		lines = append(lines, cursor+line)
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
		errorStyle.Render("Delete this record?"),
		"",
		"  "+description,
		model.footer("y delete · n cancel"),
	)
}

// --- helpers ---------------------------------------------------------------

func statusFor(err error) string {
	if rateLimited, isRateLimited := gateway.IsRateLimited(err); isRateLimited {
		return "⚠ DNSimple rate limit hit - resuming at " + rateLimited.ResumeAt.Local().Format("15:04")
	}
	if errors.Is(err, gateway.ErrUpstreamDown) {
		return "⚠ DNSimple is struggling - backing off, try again shortly"
	}
	var invalid zones.InvalidRecordError
	if errors.As(err, &invalid) {
		return "✗ " + invalid.Reason
	}
	message := err.Error()
	if len(message) > 100 {
		message = message[:100] + "..."
	}
	return "✗ " + message
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
