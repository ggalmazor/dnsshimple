package sshui

import (
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"dnsshimple/core/domains"
	"dnsshimple/core/identity"
	"dnsshimple/core/registrar"
)

var updateGoldens = flag.Bool("update", false, "rewrite the golden screen captures")

// Golden captures pin the frame layout (DESIGN §8.4): the renderer writes
// to io.Discard, so styles degrade to plain text and the capture is stable.
func goldenModel() sessionModel {
	model := sessionModel{
		state:       stateLinked,
		fingerprint: "SHA256:golden",
		appCtx:      context.Background(),
		session:     identity.Session{Tenant: identity.Tenant{ID: 1, AccountID: 2416, Email: "guille@sendia.org"}},
		styles:      newStyleSet(lipgloss.NewRenderer(io.Discard)),
		width:       100,
		height:      40,
	}
	model.browser = newBrowserState()
	return model
}

func assertGolden(test *testing.T, name, rendered string) {
	test.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *updateGoldens {
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			test.Fatalf("writing %s: %v", path, err)
		}
		return
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		test.Fatalf("reading %s (run with -update to create it): %v", path, err)
	}
	if string(expected) != rendered {
		test.Fatalf("%s drifted from its golden capture; rerun with -update if intended.\n--- got ---\n%s", name, rendered)
	}
}

func TestScreensMatchTheirGoldenCaptures(test *testing.T) {
	model := goldenModel()

	model.browser.screen = screenHome
	assertGolden(test, "home", model.renderScreen())

	model.browser.screen = screenDomains
	model.browser.domainList = []domains.Domain{
		{ID: 1, Name: "ggalmazor.com", State: "registered", AutoRenew: true, ExpiresAt: "2027-07-24T00:00:00Z"},
		{ID: 2, Name: "sendia.org", State: "hosted"},
	}
	assertGolden(test, "domains", model.renderScreen())

	model.browser.screen = screenTLDs
	model.browser.tldList = []registrar.TLD{
		{Name: "com", RegistrationEnabled: true, TransferEnabled: true, WhoisPrivacy: true, IDN: true, MinimumYears: 1},
		{Name: "dev", RegistrationEnabled: true, TransferEnabled: true, MinimumYears: 1},
	}
	assertGolden(test, "tlds", model.renderScreen())

	model.browser.screen = screenAccountPicker
	model.browser.accountList = []identity.Tenant{
		{ID: 1, AccountID: 2416, Email: "guille@sendia.org"},
		{ID: 2, AccountID: 107677, Email: "guille@dnsimple.com"},
	}
	assertGolden(test, "accounts", model.renderScreen())
}
