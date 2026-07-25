package sshui

import (
	"io"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"dnsshimple/core/zones"
)

func browserKey(name string) tea.KeyMsg {
	special := map[string]tea.KeyType{
		"up": tea.KeyUp, "down": tea.KeyDown, "esc": tea.KeyEscape,
		"enter": tea.KeyEnter, "tab": tea.KeyTab,
	}
	if keyType, isSpecial := special[name]; isSpecial {
		return tea.KeyMsg{Type: keyType}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func pressKeys(test *testing.T, model sessionModel, names ...string) sessionModel {
	test.Helper()
	for _, name := range names {
		updated, _ := model.updateBrowser(browserKey(name))
		model = updated.(sessionModel)
	}
	return model
}

func filterTestModel() sessionModel {
	model := sessionModel{styles: newStyleSet(lipgloss.NewRenderer(io.Discard))}
	model.browser = newBrowserState()
	return model
}

func TestClosingTheFilterKeepsTheSelectedItemSelected(test *testing.T) {
	model := filterTestModel()
	model.browser.screen = screenZones
	model.browser.zoneList = []zones.Zone{{Name: "alpha.com"}, {Name: "beta.org"}, {Name: "gamma.org"}}

	// Filter down to the two .org zones, select the second, close the filter.
	model = pressKeys(test, model, "/", "o", "r", "g", "down", "esc")

	if model.browser.listFilterOn {
		test.Fatal("esc must close the filter")
	}
	if selected := model.browser.zoneList[model.browser.zoneCursor].Name; selected != "gamma.org" {
		test.Fatalf("the selection must survive closing the filter; cursor moved to %q", selected)
	}
}

func TestNavigatingAwayUnderAFilterKeepsTheSelectedItemSelected(test *testing.T) {
	model := filterTestModel()
	model.browser.screen = screenZones
	model.browser.zoneList = []zones.Zone{{Name: "alpha.com"}, {Name: "beta.org"}, {Name: "gamma.org"}}

	model = pressKeys(test, model, "/", "o", "r", "g", "down")
	updated, _ := model.updateBrowser(recordsLoadedMessage{zone: "gamma.org"})
	model = updated.(sessionModel)
	// The next key hits the screen-change guard that discards the filter.
	model = pressKeys(test, model, "esc")

	if model.browser.listFilterOn {
		test.Fatal("leaving the screen must discard the filter")
	}
	if selected := model.browser.zoneList[model.browser.zoneCursor].Name; selected != "gamma.org" {
		test.Fatalf("the selection must survive leaving the screen; cursor moved to %q", selected)
	}
}

func TestExecutingAFocusedActionWhileFilteringRunsItsHandler(test *testing.T) {
	model := filterTestModel()
	model.browser.screen = screenRecords
	model.browser.currentZone = "alpha.com"
	model.browser.records = []zones.Record{{Name: "www", Type: "A", Content: "1.1.1.1"}}

	// Tab twice reaches the "n new" action; enter must run it, not type "n".
	model = pressKeys(test, model, "/", "w", "tab", "tab", "enter")

	if model.browser.screen != screenForm {
		test.Fatalf("enter on a focused action must run its handler; still on screen %d", model.browser.screen)
	}
	if value := model.browser.listFilter.Value(); value != "w" {
		test.Fatalf("the action's key must not land in the filter input; filter reads %q", value)
	}
}
