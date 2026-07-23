// Package jobs is the driven adapter for async workflows: River executes
// them on Postgres, so a poll survives restarts and redeploys and retries
// with backoff without extra infrastructure (DESIGN.md §3.2).
package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/riverqueue/river"

	"dnsshimple/core/gateway"
	"dnsshimple/core/identity"
	"dnsshimple/core/registrar"
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

	step := decidePollStep(polled.Error(), !polled.Failed() && polled.Value().Terminal(), time.Now(), 15*time.Second)
	if step.snoozeFor > 0 {
		return river.JobSnooze(step.snoozeFor)
	}
	return step.failure
}

// pollStep is the worker's verdict, separated from River so the decision
// logic tests without a database: snooze, fail (River retries), or done.
// Shared by every state-polling workflow (registrations, certificates).
type pollStep struct {
	snoozeFor time.Duration
	failure   error
}

func decidePollStep(failure error, terminal bool, now time.Time, pendingSnooze time.Duration) pollStep {
	if failure != nil {
		if rateLimited, isRateLimited := gateway.IsRateLimited(failure); isRateLimited {
			// Honor the budget: sleep to DNSimple's own reset (plus margin)
			// instead of burning retries against a closed door.
			wait := rateLimited.ResumeAt.Sub(now) + 10*time.Second
			if wait < time.Minute {
				wait = time.Minute
			}
			return pollStep{snoozeFor: wait}
		}
		if errors.Is(failure, gateway.ErrUpstreamDown) {
			return pollStep{snoozeFor: time.Minute}
		}
		return pollStep{failure: failure}
	}
	if terminal {
		return pollStep{} // the Component already logged and counted it
	}
	return pollStep{snoozeFor: pendingSnooze}
}
