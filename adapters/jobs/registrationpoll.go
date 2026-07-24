// Package jobs is the driven adapter for async workflows: River executes
// them on Postgres, so a poll survives restarts and redeploys and retries
// with backoff without extra infrastructure (DESIGN.md §3.2).
package jobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"dnsshimple/core/identity"
	"dnsshimple/core/registrar"
	"dnsshimple/core/workflows"
)

type RegistrationPollArgs struct {
	TenantID       int64  `json:"tenant_id"`
	DomainName     string `json:"domain_name"`
	RegistrationID int64  `json:"registration_id"`
}

func (RegistrationPollArgs) Kind() string { return "registration_poll" }

type RegistrationPollWorker struct {
	river.WorkerDefaults[RegistrationPollArgs]
	Identity  *identity.Component
	Registrar *registrar.Component
}

func (worker *RegistrationPollWorker) Work(ctx context.Context, job *river.Job[RegistrationPollArgs]) error {
	session := worker.Identity.SessionForTenant(ctx, job.Args.TenantID)
	if session.Failed() {
		return session.Error()
	}
	polled := worker.Registrar.PollRegistration(ctx,
		session.Value(), job.Args.DomainName, job.Args.RegistrationID)

	step := workflows.DecidePollStep(polled.Error(), !polled.Failed() && polled.Value().Terminal(), time.Now(), 15*time.Second)
	if step.SnoozeFor > 0 {
		return river.JobSnooze(step.SnoozeFor)
	}
	return step.Failure
}
