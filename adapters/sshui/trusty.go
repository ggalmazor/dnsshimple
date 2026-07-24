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

// renderHalfBlocks draws generated pixel art (logoart.go and friends): one
// character per cell, foreground painting the upper pixel and background
// the lower.
func renderHalfBlocks(renderer *lipgloss.Renderer, pixels [][][2]string) string {
	rows := make([]string, 0, len(pixels))
	for _, row := range pixels {
		var line strings.Builder
		for _, cell := range row {
			upper, lower := cell[0], cell[1]
			switch {
			case upper == "" && lower == "":
				line.WriteString(" ")
			case lower == "":
				line.WriteString(renderer.NewStyle().Foreground(lipgloss.Color(upper)).Render("▀"))
			case upper == "":
				line.WriteString(renderer.NewStyle().Foreground(lipgloss.Color(lower)).Render("▄"))
			default:
				line.WriteString(renderer.NewStyle().
					Foreground(lipgloss.Color(upper)).
					Background(lipgloss.Color(lower)).
					Render("▀"))
			}
		}
		rows = append(rows, line.String())
	}
	return strings.Join(rows, "\n")
}
