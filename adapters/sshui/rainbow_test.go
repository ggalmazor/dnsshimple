package sshui

import (
	"io"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func keyFor(name string) tea.KeyMsg {
	special := map[string]tea.KeyType{
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	}
	if keyType, isSpecial := special[name]; isSpecial {
		return tea.KeyMsg{Type: keyType}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func typeKeys(model sessionModel, names []string) sessionModel {
	for _, name := range names {
		updated, _ := model.homeKeys(keyFor(name))
		model = updated.(sessionModel)
	}
	return model
}

func TestTheKonamiCodeTogglesRainbowModeOnTheHomeScreen(test *testing.T) {
	model := sessionModel{styles: newStyleSet(lipgloss.NewRenderer(io.Discard))}

	model = typeKeys(model, konamiCode[:])
	if !model.browser.rainbowMode {
		test.Fatal("the Konami code must switch rainbow mode on")
	}
	// The code works as a suffix of whatever was typed before it, like the
	// original cheat did on the title screen.
	model = typeKeys(model, append([]string{"j", "k"}, konamiCode[:]...))
	if model.browser.rainbowMode {
		test.Fatal("the Konami code must switch rainbow mode off again")
	}
	if model.framed("hello") == "" {
		test.Fatal("the rainbow frame must still render")
	}
}

func TestAPartialCheatSequenceLeavesRainbowModeOff(test *testing.T) {
	model := sessionModel{styles: newStyleSet(lipgloss.NewRenderer(io.Discard))}
	model = typeKeys(model, konamiCode[:len(konamiCode)-1])
	if model.browser.rainbowMode {
		test.Fatal("an incomplete sequence must not toggle")
	}
}
