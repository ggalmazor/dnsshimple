package admintui

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"dnsshimple/adapters/memory"
	"dnsshimple/core/admin"
	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
	"dnsshimple/kit"
)

// Grey-box at the model port: real Components over in-memory port fakes,
// keys and messages in, screens and facade effects out.

type fakeStatistics struct{}

func (fakeStatistics) DatabaseBytes(context.Context) kit.Outcome[int64] {
	return kit.Success[int64](5 << 20)
}

func (fakeStatistics) Tables(context.Context) kit.Outcome[[]admin.TableStat] {
	return kit.Success([]admin.TableStat{{Name: "tenants", LiveRows: 3, TotalBytes: 65536}})
}

type fakeJobQueue struct {
	jobs    []admin.Job
	retried []int64
	deleted []int64
}

func (queue *fakeJobQueue) CountsByState(context.Context) kit.Outcome[map[string]int64] {
	return kit.Success(map[string]int64{"available": 2, "discarded": 1})
}

func (queue *fakeJobQueue) TroubledJobs(context.Context, int) kit.Outcome[[]admin.Job] {
	return kit.Success(queue.jobs)
}

func (queue *fakeJobQueue) Retry(_ context.Context, jobID int64) kit.Outcome[kit.Nothing] {
	queue.retried = append(queue.retried, jobID)
	return kit.Done()
}

func (queue *fakeJobQueue) Delete(_ context.Context, jobID int64) kit.Outcome[kit.Nothing] {
	queue.deleted = append(queue.deleted, jobID)
	return kit.Done()
}

type plainVault struct{}

func (plainVault) Seal(accessToken string) kit.Outcome[[]byte] {
	return kit.Success([]byte(accessToken))
}

func (plainVault) Open(sealedToken []byte) kit.Outcome[string] {
	return kit.Success(string(sealedToken))
}

type harness struct {
	model      tea.Model
	queue      *fakeJobQueue
	repository *memory.IdentityRepository
}

func newHarness(test *testing.T) *harness {
	test.Helper()
	queue := &fakeJobQueue{}
	repository := memory.NewIdentityRepository()
	identityComponent := identity.NewComponent(repository, plainVault{}, nil, telemetry.NewMetrics())
	components := Components{
		Admin:    admin.NewComponent(fakeStatistics{}, queue, identityComponent),
		Identity: identityComponent,
	}
	return &harness{
		model:      NewModel(context.Background(), components, lipgloss.NewRenderer(io.Discard)),
		queue:      queue,
		repository: repository,
	}
}

func (harness *harness) deliver(test *testing.T, message tea.Msg) tea.Cmd {
	test.Helper()
	updated, command := harness.model.Update(message)
	harness.model = updated
	return command
}

// press sends the key and, when a command comes back, runs it and feeds
// its message in - the synchronous version of Bubble Tea's loop.
func (harness *harness) press(test *testing.T, name string) {
	test.Helper()
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	if command := harness.deliver(test, key); command != nil {
		harness.deliver(test, command())
	}
}

func (harness *harness) admin() adminModel {
	return harness.model.(adminModel)
}

func TestTheDashboardMergesOverviewSourcesAndFlagsDegradation(test *testing.T) {
	harness := newHarness(test)

	harness.deliver(test, overviewLoadedMessage{overview: admin.Overview{
		DatabaseBytes: 5 << 20,
		Tables:        []admin.TableStat{{Name: "tenants", LiveRows: 3, TotalBytes: 65536}},
		JobCounts:     map[string]int64{"available": 2},
		Partial:       []error{errors.New("stats unreachable")},
	}})

	view := harness.model.View()
	for _, expected := range []string{"5.0 MiB", "tenants", "available 2", "stats unreachable"} {
		if !strings.Contains(view, expected) {
			test.Fatalf("dashboard must show %q, got:\n%s", expected, view)
		}
	}
}

func TestUnlinkingABindingConfirmsFirstAndThenExecutes(test *testing.T) {
	// Given one bound key shown on the keys screen
	harness := newHarness(test)
	bound := harness.repository.BindTenant(context.Background(), "SHA256:laptop",
		identity.RemoteIdentity{AccountID: 2416, Email: "guille@sendia.org"}, []byte("sealed"))
	if bound.Failed() {
		test.Fatalf("seeding the binding: %v", bound.Error())
	}
	harness.press(test, "2")
	if len(harness.admin().bindings) != 1 {
		test.Fatalf("expected the seeded binding on screen, got %+v", harness.admin().bindings)
	}

	// When x is pressed, nothing happens until y confirms
	harness.press(test, "x")
	if harness.admin().screen != screenConfirm {
		test.Fatal("x must open the confirmation screen")
	}
	if outcome := harness.repository.TenantBySSHKey(context.Background(), "SHA256:laptop"); outcome.Failed() {
		test.Fatal("the binding must survive until the confirmation")
	}
	harness.press(test, "y")

	// Then the binding is gone and the status says so
	if outcome := harness.repository.TenantBySSHKey(context.Background(), "SHA256:laptop"); !outcome.Failed() {
		test.Fatal("confirming must unlink the binding")
	}
	if !strings.Contains(harness.admin().status, "unlink done") {
		test.Fatalf("expected an unlink confirmation, got %q", harness.admin().status)
	}
}

func TestCancellingAConfirmationLeavesEverythingInPlace(test *testing.T) {
	harness := newHarness(test)
	_ = harness.repository.BindTenant(context.Background(), "SHA256:laptop",
		identity.RemoteIdentity{AccountID: 2416, Email: "guille@sendia.org"}, []byte("sealed"))
	harness.press(test, "2")

	harness.press(test, "x")
	harness.press(test, "n")

	if harness.admin().screen != screenBindings {
		test.Fatal("n must return to the keys screen")
	}
	if outcome := harness.repository.TenantBySSHKey(context.Background(), "SHA256:laptop"); outcome.Failed() {
		test.Fatal("cancelling must keep the binding")
	}
}

func TestJobRetryIsImmediateAndDeleteConfirmsFirst(test *testing.T) {
	// Given two troubled jobs on the jobs screen
	harness := newHarness(test)
	harness.queue.jobs = []admin.Job{
		{ID: 7, Kind: "registration_poll", State: "discarded", LastError: "boom"},
		{ID: 9, Kind: "transfer_poll", State: "cancelled"},
	}
	harness.press(test, "3")

	// When t retries the selected job, the facade is hit straight away
	harness.press(test, "t")
	if len(harness.queue.retried) != 1 || harness.queue.retried[0] != 7 {
		test.Fatalf("t must retry the selected job, got %v", harness.queue.retried)
	}

	// And d on the next job demands a confirmation before deleting
	harness.press(test, "j")
	harness.press(test, "d")
	if harness.admin().screen != screenConfirm {
		test.Fatal("d must open the confirmation screen")
	}
	if len(harness.queue.deleted) != 0 {
		test.Fatal("nothing may be deleted before the confirmation")
	}
	harness.press(test, "y")
	if len(harness.queue.deleted) != 1 || harness.queue.deleted[0] != 9 {
		test.Fatalf("y must delete the selected job, got %v", harness.queue.deleted)
	}
}

func TestPruningStaleLinksReportsTheCount(test *testing.T) {
	// Given a stale link in the repository
	harness := newHarness(test)
	stale := identity.PendingLink{Code: "DUSTY123", Fingerprint: "SHA256:old",
		StartedAt: timeAgo(2 * identity.LinkCodeLifetime)}
	_ = harness.repository.SavePendingLink(context.Background(), stale)

	// When the dashboard prune runs through its confirmation
	harness.press(test, "p")
	if harness.admin().screen != screenConfirm {
		test.Fatal("p must open the confirmation screen")
	}
	harness.press(test, "y")

	// Then the status reports one pruned code
	if !strings.Contains(harness.admin().status, "pruned 1 stale link") {
		test.Fatalf("expected a prune report, got %q", harness.admin().status)
	}
}

func timeAgo(elapsed time.Duration) time.Time {
	return time.Now().Add(-elapsed)
}
