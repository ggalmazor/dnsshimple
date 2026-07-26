// Package admintui is the operator console: a Bubble Tea program run on
// the running container with docker exec (the distroless image has no
// shell, so exec targets the binary itself). It renders and dispatches
// into the admin and identity facades; policy stays behind them. Access
// rides the existing perimeter - SSH to the host plus docker - and the
// UI never unseals or renders a token.
package admintui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"dnsshimple/core/admin"
	"dnsshimple/core/identity"
)

// troubledJobsLimit caps the jobs list; an operator handles pages of
// failures with SQL, not a cursor.
const troubledJobsLimit = 50

type Components struct {
	Admin    *admin.Component
	Identity *identity.Component
}

// Run starts the console on the local TTY and blocks until it quits.
func Run(ctx context.Context, components Components) error {
	program := tea.NewProgram(
		NewModel(ctx, components, lipgloss.NewRenderer(os.Stdout)),
		tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := program.Run()
	return err
}

type screen int

const (
	screenDashboard screen = iota
	screenBindings
	screenJobs
	screenJobDetail
	screenConfirm
)

type styleSet struct {
	title     lipgloss.Style
	subtle    lipgloss.Style
	selected  lipgloss.Style
	errorText lipgloss.Style
	status    lipgloss.Style
}

func newStyleSet(renderer *lipgloss.Renderer) styleSet {
	return styleSet{
		title:     renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("63")),
		subtle:    renderer.NewStyle().Foreground(lipgloss.Color("241")),
		selected:  renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("212")),
		errorText: renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("203")),
		status:    renderer.NewStyle().Foreground(lipgloss.Color("214")),
	}
}

type adminModel struct {
	ctx        context.Context
	components Components
	styles     styleSet
	screen     screen
	loading    bool
	status     string
	height     int

	overview admin.Overview

	bindings      []identity.KeyBinding
	bindingCursor int

	jobs      []admin.Job
	jobCursor int

	confirmPrompt  string
	confirmCommand tea.Cmd
	confirmReturn  screen
}

func NewModel(ctx context.Context, components Components, renderer *lipgloss.Renderer) tea.Model {
	return adminModel{
		ctx:        ctx,
		components: components,
		styles:     newStyleSet(renderer),
		screen:     screenDashboard,
		loading:    true,
	}
}

func (model adminModel) Init() tea.Cmd {
	return model.loadOverview()
}

// --- messages and commands --------------------------------------------------

type overviewLoadedMessage struct {
	overview admin.Overview
}

type bindingsLoadedMessage struct {
	bindings []identity.KeyBinding
	err      error
}

type jobsLoadedMessage struct {
	jobs []admin.Job
	err  error
}

type actionDoneMessage struct {
	verb   string
	pruned int64
	err    error
}

func (model adminModel) loadOverview() tea.Cmd {
	components, ctx := model.components, model.ctx
	return func() tea.Msg {
		return overviewLoadedMessage{overview: components.Admin.Overview(ctx)}
	}
}

func (model adminModel) loadBindings() tea.Cmd {
	components, ctx := model.components, model.ctx
	return func() tea.Msg {
		outcome := components.Identity.Bindings(ctx)
		return bindingsLoadedMessage{bindings: outcome.Value(), err: outcome.Error()}
	}
}

func (model adminModel) loadJobs() tea.Cmd {
	components, ctx := model.components, model.ctx
	return func() tea.Msg {
		outcome := components.Admin.TroubledJobs(ctx, troubledJobsLimit)
		return jobsLoadedMessage{jobs: outcome.Value(), err: outcome.Error()}
	}
}

func (model adminModel) unlinkBinding(binding identity.KeyBinding) tea.Cmd {
	components, ctx := model.components, model.ctx
	return func() tea.Msg {
		outcome := components.Identity.UnlinkAccount(ctx, binding.Fingerprint, binding.Tenant.ID)
		return actionDoneMessage{verb: "unlink", err: outcome.Error()}
	}
}

func (model adminModel) retryJob(jobID int64) tea.Cmd {
	components, ctx := model.components, model.ctx
	return func() tea.Msg {
		outcome := components.Admin.RetryJob(ctx, jobID)
		return actionDoneMessage{verb: "retry", err: outcome.Error()}
	}
}

func (model adminModel) deleteJob(jobID int64) tea.Cmd {
	components, ctx := model.components, model.ctx
	return func() tea.Msg {
		outcome := components.Admin.DeleteJob(ctx, jobID)
		return actionDoneMessage{verb: "delete", err: outcome.Error()}
	}
}

func (model adminModel) pruneStaleLinks() tea.Cmd {
	components, ctx := model.components, model.ctx
	return func() tea.Msg {
		outcome := components.Identity.PruneStaleLinks(ctx)
		return actionDoneMessage{verb: "prune links", pruned: outcome.Value(), err: outcome.Error()}
	}
}

// reload refreshes whatever the current screen shows.
func (model adminModel) reload() tea.Cmd {
	switch model.screen {
	case screenBindings:
		return model.loadBindings()
	case screenJobs:
		return model.loadJobs()
	default:
		return model.loadOverview()
	}
}

// --- update -----------------------------------------------------------------

func (model adminModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case overviewLoadedMessage:
		model.loading = false
		model.overview = message.overview
		if len(message.overview.Partial) > 0 {
			model.status = statusFor(message.overview.Partial[0])
		}
		return model, nil

	case bindingsLoadedMessage:
		model.loading = false
		if message.err != nil {
			model.status = statusFor(message.err)
			return model, nil
		}
		model.bindings = message.bindings
		model.bindingCursor = clamp(model.bindingCursor, len(model.bindings))
		return model, nil

	case jobsLoadedMessage:
		model.loading = false
		if message.err != nil {
			model.status = statusFor(message.err)
			return model, nil
		}
		model.jobs = message.jobs
		model.jobCursor = clamp(model.jobCursor, len(model.jobs))
		return model, nil

	case actionDoneMessage:
		model.loading = false
		if message.err != nil {
			model.status = statusFor(message.err)
			return model, nil
		}
		if message.verb == "prune links" {
			model.status = fmt.Sprintf("✓ pruned %d stale link codes", message.pruned)
		} else {
			model.status = "✓ " + message.verb + " done"
		}
		model.loading = true
		return model, model.reload()

	case tea.WindowSizeMsg:
		model.height = message.Height
		return model, nil

	case tea.KeyMsg:
		return model.handleKey(message)
	}
	return model, nil
}

// visibleRows is how many list rows fit under the chrome (header with a
// possible status and loading line, blanks, position line, footer). Before
// the first WindowSizeMsg the height is unknown; assume a tall-enough
// terminal instead of collapsing the lists.
func (model adminModel) visibleRows() int {
	if model.height == 0 {
		return 20
	}
	if rows := model.height - 7; rows >= 4 {
		return rows
	}
	return 4
}

// listWindow slides a visible-sized window along the list, keeping the
// cursor near the middle - the sshui behavior, without the frame.
func listWindow(cursor, total, visible int) (int, int) {
	if total <= visible {
		return 0, total
	}
	start := cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}

func (model adminModel) positionLine(cursor, total int) string {
	if total <= model.visibleRows() {
		return ""
	}
	return model.styles.subtle.Render(fmt.Sprintf("  %d/%d", cursor+1, total))
}

func (model adminModel) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if model.screen == screenConfirm {
		switch key.String() {
		case "y":
			command := model.confirmCommand
			model.confirmCommand = nil
			model.screen = model.confirmReturn
			model.loading = true
			return model, command
		case "n", "esc":
			model.confirmCommand = nil
			model.screen = model.confirmReturn
		}
		return model, nil
	}

	if model.screen == screenJobDetail {
		switch key.String() {
		case "esc", "b", "q":
			model.screen = screenJobs
		}
		return model, nil
	}

	switch key.String() {
	case "q", "ctrl+c", "esc":
		return model, tea.Quit
	case "1":
		model.screen = screenDashboard
		model.loading = true
		model.status = ""
		return model, model.loadOverview()
	case "2":
		model.screen = screenBindings
		model.loading = true
		model.status = ""
		return model, model.loadBindings()
	case "3":
		model.screen = screenJobs
		model.loading = true
		model.status = ""
		return model, model.loadJobs()
	case "r":
		model.loading = true
		model.status = ""
		return model, model.reload()
	}

	switch model.screen {
	case screenDashboard:
		if key.String() == "p" {
			model.confirmPrompt = "Prune link codes past their lifetime? Parked link screens keep working; only dead codes go."
			model.confirmCommand = model.pruneStaleLinks()
			model.confirmReturn = screenDashboard
			model.screen = screenConfirm
		}
	case screenBindings:
		return model.bindingsKeys(key)
	case screenJobs:
		return model.jobsKeys(key)
	}
	return model, nil
}

func (model adminModel) bindingsKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		model.bindingCursor = clamp(model.bindingCursor-1, len(model.bindings))
	case "down", "j":
		model.bindingCursor = clamp(model.bindingCursor+1, len(model.bindings))
	case "x":
		if len(model.bindings) == 0 {
			return model, nil
		}
		binding := model.bindings[model.bindingCursor]
		model.confirmPrompt = fmt.Sprintf(
			"Unlink %s from %s? If it is the account's last key, the sealed token goes with it and the user relinks.",
			shortFingerprint(binding.Fingerprint), binding.Tenant.Email)
		model.confirmCommand = model.unlinkBinding(binding)
		model.confirmReturn = screenBindings
		model.screen = screenConfirm
	}
	return model, nil
}

func (model adminModel) jobsKeys(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		model.jobCursor = clamp(model.jobCursor-1, len(model.jobs))
	case "down", "j":
		model.jobCursor = clamp(model.jobCursor+1, len(model.jobs))
	case "enter":
		if len(model.jobs) == 0 {
			return model, nil
		}
		model.screen = screenJobDetail
	case "t":
		if len(model.jobs) == 0 {
			return model, nil
		}
		model.loading = true
		return model, model.retryJob(model.jobs[model.jobCursor].ID)
	case "d":
		if len(model.jobs) == 0 {
			return model, nil
		}
		job := model.jobs[model.jobCursor]
		model.confirmPrompt = fmt.Sprintf("Delete job #%d (%s, %s)? The workflow it advanced will not resume.",
			job.ID, job.Kind, job.State)
		model.confirmCommand = model.deleteJob(job.ID)
		model.confirmReturn = screenJobs
		model.screen = screenConfirm
	}
	return model, nil
}

// --- views ------------------------------------------------------------------

func (model adminModel) View() string {
	var body string
	switch model.screen {
	case screenDashboard:
		body = model.viewDashboard()
	case screenBindings:
		body = model.viewBindings()
	case screenJobs:
		body = model.viewJobs()
	case screenJobDetail:
		body = model.viewJobDetail()
	case screenConfirm:
		body = model.viewConfirm()
	}
	return body + "\n"
}

func (model adminModel) header(crumb string) string {
	line := model.styles.title.Render("DNSSHIMPLE ADMIN") + model.styles.subtle.Render(" · "+crumb)
	if model.status != "" {
		line += "\n" + model.styles.status.Render(model.status)
	}
	if model.loading {
		line += "\n" + model.styles.subtle.Render("loading...")
	}
	return line
}

func (model adminModel) footer(hints string) string {
	return "\n" + model.styles.subtle.Render(hints+" · 1 dashboard · 2 keys · 3 jobs · r refresh · q quit")
}

func (model adminModel) viewDashboard() string {
	lines := []string{model.header("dashboard"), ""}
	lines = append(lines, "  database  "+humanBytes(model.overview.DatabaseBytes))
	lines = append(lines, fmt.Sprintf("  accounts  %d tenants · %d keys · %d pending links",
		model.overview.Accounts.Tenants, model.overview.Accounts.Bindings, model.overview.Accounts.PendingLinks))
	lines = append(lines, "  jobs      "+jobCountsLine(model.overview.JobCounts))
	lines = append(lines, "")
	for _, table := range model.overview.Tables {
		lines = append(lines, fmt.Sprintf("  %-20s %8d rows %12s", table.Name, table.LiveRows, humanBytes(table.TotalBytes)))
	}
	for _, failure := range model.overview.Partial {
		lines = append(lines, model.styles.errorText.Render("  ⚠ "+failure.Error()))
	}
	return strings.Join(append(lines, model.footer("p prune stale links")), "\n")
}

func (model adminModel) viewBindings() string {
	lines := []string{model.header("ssh keys"), ""}
	if len(model.bindings) == 0 && !model.loading {
		lines = append(lines, model.styles.subtle.Render("  no key bindings"))
	}
	start, end := listWindow(model.bindingCursor, len(model.bindings), model.visibleRows())
	for index := start; index < end; index++ {
		binding := model.bindings[index]
		line := fmt.Sprintf("%-24s %-28s account %-8d %s",
			shortFingerprint(binding.Fingerprint), binding.Tenant.Email,
			binding.Tenant.AccountID, humanTime(binding.LastUsedAt))
		prefix := "  "
		if index == model.bindingCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	if position := model.positionLine(model.bindingCursor, len(model.bindings)); position != "" {
		lines = append(lines, position)
	}
	return strings.Join(append(lines, model.footer("x unlink")), "\n")
}

func (model adminModel) viewJobs() string {
	lines := []string{model.header("troubled jobs"), ""}
	if len(model.jobs) == 0 && !model.loading {
		lines = append(lines, model.styles.subtle.Render("  nothing troubled - the queue is clean"))
	}
	start, end := listWindow(model.jobCursor, len(model.jobs), model.visibleRows())
	for index := start; index < end; index++ {
		job := model.jobs[index]
		line := fmt.Sprintf("#%-6d %-24s %-10s attempt %-3d %s",
			job.ID, job.Kind, job.State, job.Attempt, truncate(job.LastError, 40))
		prefix := "  "
		if index == model.jobCursor {
			prefix = "> "
			line = model.styles.selected.Render(line)
		}
		lines = append(lines, prefix+line)
	}
	if position := model.positionLine(model.jobCursor, len(model.jobs)); position != "" {
		lines = append(lines, position)
	}
	return strings.Join(append(lines, model.footer("enter detail · t retry · d delete")), "\n")
}

func (model adminModel) viewJobDetail() string {
	if len(model.jobs) == 0 {
		return model.header("job") + "\n" + model.styles.subtle.Render("  gone")
	}
	job := model.jobs[clamp(model.jobCursor, len(model.jobs))]
	lines := []string{model.header(fmt.Sprintf("job #%d", job.ID)), ""}
	lines = append(lines,
		"  kind       "+job.Kind,
		"  state      "+job.State,
		fmt.Sprintf("  attempt    %d", job.Attempt),
		"  scheduled  "+job.ScheduledAt.Format("Jan 2 15:04:05"),
		"  args       "+job.ArgsJSON,
		"")
	if len(job.Errors) == 0 {
		lines = append(lines, model.styles.subtle.Render("  no recorded errors"))
	}
	for _, attempt := range job.Errors {
		lines = append(lines, wrap(attempt, 76, "  ")...)
	}
	return strings.Join(append(lines, "\n"+model.styles.subtle.Render("esc back")), "\n")
}

func (model adminModel) viewConfirm() string {
	return strings.Join([]string{
		model.header("confirm"),
		"",
		model.styles.errorText.Render(model.confirmPrompt),
		"",
		model.styles.subtle.Render("y confirm · n cancel"),
	}, "\n")
}

// --- helpers ----------------------------------------------------------------

// statusFor is the one place errors become human here (errors.md rule 3);
// admin failures are local infrastructure, so the raw cause is the action.
func statusFor(err error) string {
	return "✗ " + truncate(err.Error(), 100)
}

func jobCountsLine(counts map[string]int64) string {
	if len(counts) == 0 {
		return "none"
	}
	states := make([]string, 0, len(counts))
	for state := range counts {
		states = append(states, state)
	}
	sort.Strings(states)
	parts := make([]string, 0, len(states))
	for _, state := range states {
		parts = append(parts, fmt.Sprintf("%s %d", state, counts[state]))
	}
	return strings.Join(parts, " · ")
}

func shortFingerprint(fingerprint string) string {
	return truncate(fingerprint, 24)
}

// wrap breaks text into prefixed lines of at most width characters,
// splitting on spaces where it can.
func wrap(text string, width int, prefix string) []string {
	var lines []string
	for len(text) > width {
		cut := strings.LastIndex(text[:width], " ")
		if cut <= 0 {
			cut = width
		}
		lines = append(lines, prefix+strings.TrimRight(text[:cut], " "))
		text = strings.TrimLeft(text[cut:], " ")
	}
	if text != "" {
		lines = append(lines, prefix+text)
	}
	return lines
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit-3] + "..."
}

func humanBytes(size int64) string {
	switch {
	case size >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(size)/(1<<30))
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(size)/(1<<10))
	default:
		return fmt.Sprintf("%d B", size)
	}
}

func humanTime(when time.Time) string {
	if when.IsZero() {
		return "never"
	}
	return when.Format("Jan 2 15:04")
}

func clamp(cursor, length int) int {
	if length == 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= length {
		return length - 1
	}
	return cursor
}
