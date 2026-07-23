package sshui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderTrusty draws the mascot with half-block characters: each cell is one
// character whose foreground paints the upper pixel and background the lower
// one. Styles come from the session renderer, so truecolor terminals get
// Trusty in full DNSimple red and colorless ones get a quiet silhouette
// instead of escape-code soup. Rendered once per session and cached on the
// model - View runs on every spinner frame.
func renderTrusty(renderer *lipgloss.Renderer) string {
	rows := make([]string, 0, len(trustyPixels))
	for _, row := range trustyPixels {
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
