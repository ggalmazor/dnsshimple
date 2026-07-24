package sshui

import (
	"dnsshimple/core/identity"
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type linkedAccountsMessage struct {
	tenants []identity.Tenant
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
	case "x":
		if len(browser.accountList) == 0 {
			return model, nil
		}
		chosen := browser.accountList[browser.accountCursor]
		if chosen.ID == model.session.Tenant.ID {
			// Unlinking the ground a session stands on would leave it
			// with a token its own picker just revoked - switch first.
			browser.status = "✗ switch to another account before unlinking this one"
			return model, nil
		}
		browser.confirmPrompt = "Unlink " + accountLabel(chosen) + " from this key? Its sealed token is removed when no other key links it."
		browser.confirmCommand = model.unlinkAccount(chosen.ID)
		browser.confirmReturn = screenAccountPicker
		browser.screen = screenConfirmAction
	}
	return model, nil
}

// accountLabel prefers what a human recognizes: the email, then the id.
// The API exposes no account name (AccountSerializer emits id, email, and
// plan only), so the email is the best label DNSimple hands us.
func accountLabel(tenant identity.Tenant) string {
	if tenant.Email != "" {
		return tenant.Email
	}
	return "#" + strconv.FormatInt(tenant.AccountID, 10)
}

func (model sessionModel) unlinkAccount(tenantID int64) tea.Cmd {
	component, fingerprint, ctx := model.components.Identity, model.fingerprint, model.appCtx
	return func() tea.Msg {
		outcome := component.UnlinkAccount(ctx, fingerprint, tenantID)
		return accountUnlinkedMessage{err: outcome.Error()}
	}
}

type accountUnlinkedMessage struct {
	err error
}

func (model sessionModel) viewAccountPicker() string {
	browser := model.browser
	lines := []string{model.header("accounts"), ""}
	for index, tenant := range browser.accountList {
		line := fmt.Sprintf("%-40.40s #%d", accountLabel(tenant), tenant.AccountID)
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
