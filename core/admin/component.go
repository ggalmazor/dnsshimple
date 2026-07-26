package admin

import (
	"context"
	"log/slog"
	"sync"

	"dnsshimple/kit"
)

// Component is the admin facade the admin TUI talks to.
type Component struct {
	statistics  Statistics
	jobs        JobQueue
	accounts    Accounts
	maintenance Maintenance
}

func NewComponent(statistics Statistics, jobs JobQueue, accounts Accounts, maintenance Maintenance) *Component {
	return &Component{statistics: statistics, jobs: jobs, accounts: accounts, maintenance: maintenance}
}

// Overview is a sync BTX: the dashboard's three reads fanned out
// concurrently and degraded per source - an unreachable statistics view
// must not blank the job counts, and vice versa.
func (component *Component) Overview(ctx context.Context) Overview {
	var overview Overview
	var guard sync.Mutex
	var pending sync.WaitGroup
	fetch := func(load func() error) {
		pending.Add(1)
		go func() {
			defer pending.Done()
			if err := load(); err != nil {
				guard.Lock()
				overview.Partial = append(overview.Partial, err)
				guard.Unlock()
			}
		}()
	}
	fetch(func() error {
		outcome := component.statistics.DatabaseBytes(ctx)
		if outcome.Failed() {
			return outcome.Error()
		}
		overview.DatabaseBytes = outcome.Value()
		return nil
	})
	fetch(func() error {
		outcome := component.statistics.Tables(ctx)
		if outcome.Failed() {
			return outcome.Error()
		}
		overview.Tables = outcome.Value()
		return nil
	})
	fetch(func() error {
		outcome := component.jobs.CountsByState(ctx)
		if outcome.Failed() {
			return outcome.Error()
		}
		overview.JobCounts = outcome.Value()
		return nil
	})
	fetch(func() error {
		outcome := component.accounts.Census(ctx)
		if outcome.Failed() {
			return outcome.Error()
		}
		overview.Accounts = outcome.Value()
		return nil
	})
	pending.Wait()
	if len(overview.Partial) > 0 {
		slog.WarnContext(ctx, "admin Overview degraded", "failed_sources", len(overview.Partial))
	}
	return overview
}

// TroubledJobs lists the jobs an operator may need to act on.
func (component *Component) TroubledJobs(ctx context.Context, limit int) kit.Outcome[[]Job] {
	return component.jobs.TroubledJobs(ctx, limit)
}

// RecentJobs lists the newest jobs whatever their state.
func (component *Component) RecentJobs(ctx context.Context, limit int) kit.Outcome[[]Job] {
	return component.jobs.RecentJobs(ctx, limit)
}

// VacuumAnalyze runs database upkeep and refreshes the planner
// estimates behind the dashboard's table statistics.
func (component *Component) VacuumAnalyze(ctx context.Context) kit.Outcome[kit.Nothing] {
	vacuumed := component.maintenance.VacuumAnalyze(ctx)
	if !vacuumed.Failed() {
		slog.InfoContext(ctx, "admin vacuumed the database")
	}
	return vacuumed
}

// RetryJob puts a finished or failing job back in line immediately.
func (component *Component) RetryJob(ctx context.Context, jobID int64) kit.Outcome[kit.Nothing] {
	retried := component.jobs.Retry(ctx, jobID)
	if !retried.Failed() {
		slog.InfoContext(ctx, "admin retried job", "job_id", jobID)
	}
	return retried
}

// DeleteJob removes a job outright; River refuses while it is running.
func (component *Component) DeleteJob(ctx context.Context, jobID int64) kit.Outcome[kit.Nothing] {
	deleted := component.jobs.Delete(ctx, jobID)
	if !deleted.Failed() {
		slog.InfoContext(ctx, "admin deleted job", "job_id", jobID)
	}
	return deleted
}
