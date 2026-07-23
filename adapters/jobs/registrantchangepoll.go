package jobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"dnsshimple/core/identity"
	"dnsshimple/core/registrar"
)

type RegistrantChangePollArgs struct {
	TenantID int64 `json:"tenant_id"`
	ChangeID int64 `json:"change_id"`
}

func (RegistrantChangePollArgs) Kind() string { return "registrant_change_poll" }

type RegistrantChangePollWorker struct {
	river.WorkerDefaults[RegistrantChangePollArgs]
	Identity  *identity.Component
	Registrar *registrar.Component
}

func (worker *RegistrantChangePollWorker) Work(ctx context.Context, job *river.Job[RegistrantChangePollArgs]) error {
	session := worker.Identity.SessionForTenant(ctx, job.Args.TenantID)
	if session.Failed() {
		return session.Error()
	}
	polled := worker.Registrar.PollRegistrantChange(ctx, session.Value(), job.Args.ChangeID)

	// Registrant changes can wait on email approvals for days; poll gently.
	step := decidePollStep(polled.Error(), !polled.Failed() && polled.Value().Terminal(), time.Now(), 15*time.Minute)
	if step.snoozeFor > 0 {
		return river.JobSnooze(step.snoozeFor)
	}
	return step.failure
}
