package jobs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"dnsshimple/core/admin"
	"dnsshimple/kit"
)

// Admin implements admin.JobQueue, the River administration port. State
// transitions (retry, delete) go through the River client so job
// lifecycle stays River's business; the one direct read on river_job -
// counts by state - is a GROUP BY over the table this adapter already
// operates, because River's list API cannot count without paging.
type Admin struct {
	client *river.Client[pgx.Tx]
	pool   *pgxpool.Pool
}

func NewAdmin(client *river.Client[pgx.Tx], pool *pgxpool.Pool) Admin {
	return Admin{client: client, pool: pool}
}

var _ admin.JobQueue = Admin{}

func (administration Admin) CountsByState(ctx context.Context) kit.Outcome[map[string]int64] {
	rows, err := administration.pool.Query(ctx,
		`SELECT state::text, count(*) FROM river_job GROUP BY state`)
	if err != nil {
		return kit.Failure[map[string]int64](fmt.Errorf("counting jobs by state: %w", err))
	}
	defer rows.Close()
	counts := map[string]int64{}
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return kit.Failure[map[string]int64](fmt.Errorf("scanning a job count: %w", err))
		}
		counts[state] = count
	}
	return kit.Success(counts)
}

func (administration Admin) TroubledJobs(ctx context.Context, limit int) kit.Outcome[[]admin.Job] {
	listed, err := administration.client.JobList(ctx, river.NewJobListParams().
		States(rivertype.JobStateDiscarded, rivertype.JobStateCancelled, rivertype.JobStateRetryable).
		OrderBy(river.JobListOrderByTime, river.SortOrderDesc).
		First(limit))
	if err != nil {
		return kit.Failure[[]admin.Job](fmt.Errorf("listing troubled jobs: %w", err))
	}
	jobs := make([]admin.Job, 0, len(listed.Jobs))
	for _, row := range listed.Jobs {
		jobs = append(jobs, jobFromRow(row))
	}
	return kit.Success(jobs)
}

func (administration Admin) Retry(ctx context.Context, jobID int64) kit.Outcome[kit.Nothing] {
	if _, err := administration.client.JobRetry(ctx, jobID); err != nil {
		return kit.Failure[kit.Nothing](fmt.Errorf("retrying job %d: %w", jobID, err))
	}
	return kit.Done()
}

func (administration Admin) Delete(ctx context.Context, jobID int64) kit.Outcome[kit.Nothing] {
	if _, err := administration.client.JobDelete(ctx, jobID); err != nil {
		return kit.Failure[kit.Nothing](fmt.Errorf("deleting job %d: %w", jobID, err))
	}
	return kit.Done()
}

func jobFromRow(row *rivertype.JobRow) admin.Job {
	job := admin.Job{
		ID:          row.ID,
		Kind:        row.Kind,
		State:       string(row.State),
		Attempt:     row.Attempt,
		ScheduledAt: row.ScheduledAt,
	}
	if len(row.Errors) > 0 {
		job.LastError = row.Errors[len(row.Errors)-1].Error
	}
	return job
}
