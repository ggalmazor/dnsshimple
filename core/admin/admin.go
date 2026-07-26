// Package admin is the operational-oversight Component behind the local
// admin TUI: database statistics and River job administration. It owns no
// DNSimple API area and no tables of its own - identity-owned data stays
// behind the identity facade, which drivers call directly.
package admin

import (
	"context"
	"time"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

// TableStat is one user table as Postgres accounts for it. LiveRows is
// the planner's live-tuple estimate, not a COUNT(*): reading it never
// scans another component's table.
type TableStat struct {
	Name       string
	LiveRows   int64
	TotalBytes int64
}

// Job is one River job as the admin surface shows it. LastError is the
// newest attempt's message for list rows; Errors is the full attempt
// history and ArgsJSON the job's payload, both for the detail view.
type Job struct {
	ID          int64
	Kind        string
	State       string
	Attempt     int
	LastError   string
	Errors      []string
	ArgsJSON    string
	ScheduledAt time.Time
}

// Overview is the dashboard aggregate: per-source values with the errors
// of the sources that failed, so one failing read never blanks the rest
// (the domains.DomainSettings stance). Accounts is exact where Tables
// only estimates.
type Overview struct {
	DatabaseBytes int64
	Tables        []TableStat
	JobCounts     map[string]int64
	Accounts      identity.Census
	Partial       []error
}

// Statistics is the database-metadata port. Implementations read Postgres
// catalog views only, never any component's tables.
type Statistics interface {
	DatabaseBytes(ctx context.Context) kit.Outcome[int64]
	Tables(ctx context.Context) kit.Outcome[[]TableStat]
}

// Accounts is the narrow slice of identity the Overview BTX composes;
// the identity Component satisfies it. Constructor-injected, so the
// dependency stays visible in cmd/dnsshimple.
type Accounts interface {
	Census(ctx context.Context) kit.Outcome[identity.Census]
}

// JobQueue is the River administration port adapters/jobs implements.
// There is deliberately no prune verb: River's own retention cleaner
// removes finished jobs on the running server.
type JobQueue interface {
	CountsByState(ctx context.Context) kit.Outcome[map[string]int64]
	// TroubledJobs lists jobs that ended badly or keep failing:
	// discarded, cancelled, and retryable, newest first.
	TroubledJobs(ctx context.Context, limit int) kit.Outcome[[]Job]
	// RecentJobs lists jobs in any state, newest first - the healthy
	// queue made visible.
	RecentJobs(ctx context.Context, limit int) kit.Outcome[[]Job]
	Retry(ctx context.Context, jobID int64) kit.Outcome[kit.Nothing]
	Delete(ctx context.Context, jobID int64) kit.Outcome[kit.Nothing]
}
