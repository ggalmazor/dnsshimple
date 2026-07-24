package sshui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderTrusty draws the hand-drawn mascot map: every cell is a space whose
// background carries the palette color, built with the session renderer so
// capable terminals get the full palette and colorless ones a quiet blank
// instead of escape-code soup. Rendered once per session and cached on the
// model - View runs on every spinner frame.
func renderTrusty(renderer *lipgloss.Renderer) string {
	// One style per palette entry, not per cell.
	styles := make(map[rune]lipgloss.Style, len(trustyPalette))
	for glyph, color := range trustyPalette {
		styles[glyph] = renderer.NewStyle().Background(lipgloss.Color(color))
	}

	rows := make([]string, 0, len(trustyMap))
	for _, mapRow := range trustyMap {
		var line strings.Builder
		for _, glyph := range mapRow {
			if style, colored := styles[glyph]; colored {
				line.WriteString(style.Render(" "))
			} else {
				line.WriteString(" ")
			}
		}
		rows = append(rows, line.String())
	}
	return strings.Join(rows, "\n")
}
