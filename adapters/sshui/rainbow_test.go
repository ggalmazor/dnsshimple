package sshui

import (
	"io"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func typeRunes(model sessionModel, typed string) sessionModel {
	for _, r := range typed {
		updated, _ := model.homeKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(sessionModel)
	}
	return model
}

func TestIdkfaTogglesRainbowModeOnTheHomeScreen(test *testing.T) {
	model := sessionModel{styles: newStyleSet(lipgloss.NewRenderer(io.Discard))}

	model = typeRunes(model, "idkfa")
	if !model.browser.rainbowMode {
		test.Fatal("typing idkfa must switch rainbow mode on")
	}
	// The sequence works as a suffix of whatever was typed before, like the
	// original cheat did mid-game.
	model = typeRunes(model, "zzidkfa")
	if model.browser.rainbowMode {
		test.Fatal("typing idkfa again must switch rainbow mode off")
	}
	if model.framed("hello") == "" {
		test.Fatal("rainbow frame must still render")
	}
}

func TestAPartialCheatSequenceLeavesRainbowModeOff(test *testing.T) {
	model := sessionModel{styles: newStyleSet(lipgloss.NewRenderer(io.Discard))}
	model = typeRunes(model, "idkf")
	if model.browser.rainbowMode {
		test.Fatal("an incomplete sequence must not toggle")
	}
}
