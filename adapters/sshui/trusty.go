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

// dnsimpleWordmark is the low-key corner signature; interpreted strings
// because the art itself contains backticks.
var dnsimpleWordmark = []string{
	"      _           _                 _      ",
	"     | |         (_)               | |     ",
	"   __| |_ __  ___ _ _ __ ___  _ __ | | ___ ",
	"  / _` | '_ \\/ __| | '_ ` _ \\| '_ \\| |/ _ \\",
	" | (_| | | | \\__ \\ | | | | | | |_) | |  __/",
	"  \\__,_|_| |_|___/_|_| |_| |_| .__/|_|\\___|",
	"                             | |           ",
	"                             |_|           ",
}

// renderWordmark dims the wordmark into the background register: quieter
// than content, a notch above the dot fill - and its own empty cells carry
// the same dot pattern, so the mark sits in the background instead of
// punching a blank rectangle into it.
func renderWordmark(renderer *lipgloss.Renderer) string {
	return renderWordmarkColors(renderer, func(int) lipgloss.Color { return "240" }, "237")
}

// renderWordmarkColors draws the wordmark with caller-chosen colors so party
// mode can re-tint the corner signature per render.
func renderWordmarkColors(renderer *lipgloss.Renderer, glyphColorFor func(row int) lipgloss.Color, dotColor lipgloss.Color) string {
	dot := renderer.NewStyle().Foreground(dotColor)
	rows := make([]string, 0, len(dnsimpleWordmark))
	for rowIndex, line := range dnsimpleWordmark {
		glyph := renderer.NewStyle().Foreground(glyphColorFor(rowIndex))
		var row strings.Builder
		for column, char := range line {
			switch {
			case char != ' ':
				row.WriteString(glyph.Render(string(char)))
			case column%2 == 1:
				row.WriteString(dot.Render("·"))
			default:
				row.WriteString(" ")
			}
		}
		rows = append(rows, row.String())
	}
	return strings.Join(rows, "\n")
}
