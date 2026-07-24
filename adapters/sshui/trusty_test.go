package sshui

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestTrustyRendersOneLinePerPixelRow(test *testing.T) {
	art := renderTrusty(lipgloss.NewRenderer(io.Discard))
	lines := strings.Split(art, "\n")
	if len(lines) != len(trustyMap) {
		test.Fatalf("expected %d art rows, got %d", len(trustyMap), len(lines))
	}
	for _, line := range lines {
		if line == "" {
			test.Fatal("an art row rendered empty")
		}
	}
}
