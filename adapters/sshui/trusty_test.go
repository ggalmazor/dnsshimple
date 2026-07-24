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

func TestEveryFilterableScreenAdvertisesTheFilterAction(test *testing.T) {
	for screen := range filterableScreens {
		actions, hasFooter := screenActions[screen]
		if !hasFooter {
			test.Fatalf("screen %d is filterable but has no action footer", screen)
		}
		advertised := false
		for _, entry := range actions {
			if entry.key == "/" {
				advertised = true
			}
		}
		if !advertised {
			test.Fatalf("screen %d is filterable but its footer does not offer / filter", screen)
		}
	}
}
