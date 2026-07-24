package sshui

import (
	"dnsshimple/core/contacts"
	"dnsshimple/core/pushes"
	"dnsshimple/core/registrar"
	"dnsshimple/core/secondarydns"
	"dnsshimple/core/services"
	"dnsshimple/core/templates"
	"dnsshimple/core/webhooks"
	"dnsshimple/core/zones"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

type webhooksLoadedMessage struct {
	webhooks []webhooks.Webhook
	err      error
}

type webhookMutatedMessage struct {
	verb string
	err  error
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

type secondaryLoadedMessage struct {
	primaries []secondarydns.PrimaryServer
	zones     []string
	err       error
}

type primaryMutatedMessage struct {
	verb string
	err  error
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
