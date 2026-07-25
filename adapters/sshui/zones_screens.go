package sshui

import (
	"dnsshimple/core/zones"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type zonesLoadedMessage struct {
	zones []zones.Zone
	err   error
}

type recordsLoadedMessage struct {
	zone    string
	records []zones.Record
	err     error
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

func (model sessionModel) deleteRecord(record zones.Record) tea.Cmd {
	component, session, ctx := model.components.Zones, model.session, model.appCtx
	zoneName := model.browser.currentZone
	return func() tea.Msg {
		outcome := component.DeleteRecord(ctx, session, zoneName, record.ID)
		return mutationDoneMessage{verb: "delete", err: outcome.Error()}
	}
}

type zoneActivationMessage struct {
	err error
}

type zoneDistributionMessage struct {
	zoneName    string
	distributed bool
	err         error
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

func (model sessionModel) loadZoneFile(zoneName string) tea.Cmd {
	component, session, ctx := model.components.Zones, model.session, model.appCtx
	return func() tea.Msg {
		outcome := component.ZoneFile(ctx, session, zoneName)
		return textLoadedMessage{title: zoneName + " · zone file", text: outcome.Value(),
			back: screenZones, err: outcome.Error()}
	}
}

func zoneSearchText(zone zones.Zone) string { return zone.Name }

func recordSearchText(record zones.Record) string {
	return record.Name + " " + record.Type + " " + record.Content
}

func (model sessionModel) filteredZones() []zones.Zone {
	return filterItems(model, model.browser.zoneList, zoneSearchText)
}

func (model sessionModel) filteredRecords() []zones.Record {
	return filterItems(model, model.browser.records, recordSearchText)
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

type recordDistributionMessage struct {
	label       string
	distributed bool
	err         error
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
