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
	allJobs []admin.Job
	retried []int64
	deleted []int64
}

func (queue *fakeJobQueue) CountsByState(context.Context) kit.Outcome[map[string]int64] {
	return kit.Success(map[string]int64{"available": 2, "discarded": 1})
}

func (queue *fakeJobQueue) TroubledJobs(context.Context, int) kit.Outcome[[]admin.Job] {
	return kit.Success(queue.jobs)
}

func (queue *fakeJobQueue) RecentJobs(context.Context, int) kit.Outcome[[]admin.Job] {
	return kit.Success(queue.allJobs)
}

func (queue *fakeJobQueue) Retry(_ context.Context, jobID int64) kit.Outcome[kit.Nothing] {
	queue.retried = append(queue.retried, jobID)
	return kit.Done()
}

func (queue *fakeJobQueue) Delete(_ context.Context, jobID int64) kit.Outcome[kit.Nothing] {
	queue.deleted = append(queue.deleted, jobID)
	return kit.Done()
}

type fakeMaintenance struct {
	vacuums int
}

func (maintenance *fakeMaintenance) VacuumAnalyze(context.Context) kit.Outcome[kit.Nothing] {
	maintenance.vacuums++
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
	model       tea.Model
	queue       *fakeJobQueue
	maintenance *fakeMaintenance
	repository  *memory.IdentityRepository
}

func newHarness(test *testing.T) *harness {
	test.Helper()
	queue := &fakeJobQueue{}
	maintenance := &fakeMaintenance{}
	repository := memory.NewIdentityRepository()
	identityComponent := identity.NewComponent(repository, plainVault{}, nil, telemetry.NewMetrics())
	components := Components{
		Admin:    admin.NewComponent(fakeStatistics{}, queue, identityComponent, maintenance),
		Identity: identityComponent,
	}
	return &harness{
		model:       NewModel(context.Background(), components, lipgloss.NewRenderer(io.Discard)),
		queue:       queue,
		maintenance: maintenance,
		repository:  repository,
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

func TestVacuumRunsFromTheDashboardWithoutAConfirmation(test *testing.T) {
	harness := newHarness(test)

	harness.press(test, "v")

	if harness.maintenance.vacuums != 1 {
		test.Fatalf("v must vacuum exactly once, got %d", harness.maintenance.vacuums)
	}
	if !strings.Contains(harness.admin().status, "vacuum analyze done") {
		test.Fatalf("expected a vacuum report, got %q", harness.admin().status)
	}
}

func TestEnterOpensTheJobDetailWithTheFullErrorHistory(test *testing.T) {
	// Given a troubled job whose error the list can only truncate
	harness := newHarness(test)
	longError := "gateway: DNSimple says: the registrant contact is missing a postal code and the registry refused the whole transaction"
	harness.press(test, "3")
	harness.deliver(test, jobsLoadedMessage{jobs: []admin.Job{{
		ID: 7, Kind: "registration_poll", State: "discarded", Attempt: 25,
		LastError: longError,
		Errors:    []string{"attempt 25 · Jul 26 03:12:09 · " + longError},
		ArgsJSON:  `{"tenant_id":1,"domain_name":"alpha.com","registration_id":11}`,
	}}})

	// When enter opens the detail
	harness.press(test, "enter")

	// Then the whole error and the args are on screen, and esc returns
	view := harness.model.View()
	// The wrap may break between any two words, so assert word by word.
	for _, word := range []string{"registrant", "postal", "refused", "transaction"} {
		if !strings.Contains(view, word) {
			test.Fatalf("the detail must carry the full error (missing %q):\n%s", word, view)
		}
	}
	if !strings.Contains(view, `"domain_name":"alpha.com"`) {
		test.Fatalf("the detail must carry the args JSON:\n%s", view)
	}
	harness.deliver(test, tea.KeyMsg{Type: tea.KeyEscape})
	if harness.admin().screen != screenJobs {
		test.Fatal("esc must return to the jobs list, not quit")
	}
}

func TestTheJobsScreenTogglesBetweenTroubledAndAll(test *testing.T) {
	// Given one troubled job and a healthy completed one
	harness := newHarness(test)
	harness.queue.jobs = []admin.Job{{ID: 7, Kind: "registration_poll", State: "discarded"}}
	harness.queue.allJobs = []admin.Job{
		{ID: 8, Kind: "certificate_poll", State: "completed"},
		{ID: 7, Kind: "registration_poll", State: "discarded"},
	}
	harness.press(test, "3")
	if len(harness.admin().jobs) != 1 {
		test.Fatalf("the screen must open on troubled jobs, got %+v", harness.admin().jobs)
	}

	// When a toggles to all jobs, the healthy one appears
	harness.press(test, "a")
	if len(harness.admin().jobs) != 2 {
		test.Fatalf("a must show every state, got %+v", harness.admin().jobs)
	}
	if !strings.Contains(harness.model.View(), "all jobs") {
		test.Fatal("the crumb must say all jobs")
	}

	// And a again returns to the troubled view
	harness.press(test, "a")
	if len(harness.admin().jobs) != 1 {
		test.Fatalf("a must toggle back to troubled, got %+v", harness.admin().jobs)
	}
}

func TestLongJobListsScrollInsideTheTerminalHeight(test *testing.T) {
	// Given 30 troubled jobs in a 12-row terminal
	harness := newHarness(test)
	jobs := make([]admin.Job, 30)
	for index := range jobs {
		jobs[index] = admin.Job{ID: int64(index + 1), Kind: "registration_poll", State: "discarded"}
	}
	harness.deliver(test, tea.WindowSizeMsg{Width: 80, Height: 12})
	harness.press(test, "3")
	harness.deliver(test, jobsLoadedMessage{jobs: jobs})

	// When the cursor walks deep into the list
	for range 24 {
		harness.press(test, "j")
	}

	// Then the view windows around the cursor and reports the position
	view := harness.model.View()
	if !strings.Contains(view, "#25") {
		test.Fatalf("the selected job must be visible:\n%s", view)
	}
	if strings.Contains(view, "#1 ") {
		test.Fatalf("rows far above the window must scroll out:\n%s", view)
	}
	if !strings.Contains(view, "25/30") {
		test.Fatalf("the position line must report 25/30:\n%s", view)
	}
}
