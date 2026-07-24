package jobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"dnsshimple/core/identity"
	"dnsshimple/core/registrar"
	"dnsshimple/core/workflows"
)

type TransferPollArgs struct {
	TenantID   int64  `json:"tenant_id"`
	DomainName string `json:"domain_name"`
	TransferID int64  `json:"transfer_id"`
}

func (TransferPollArgs) Kind() string { return "transfer_poll" }

type TransferPollWorker struct {
	river.WorkerDefaults[TransferPollArgs]
	Identity  *identity.Component
	Registrar *registrar.Component
}

func (worker *TransferPollWorker) Work(ctx context.Context, job *river.Job[TransferPollArgs]) error {
	session := worker.Identity.SessionForTenant(ctx, job.Args.TenantID)
	if session.Failed() {
		return session.Error()
	}
	polled := worker.Registrar.PollTransfer(ctx,
		session.Value(), job.Args.DomainName, job.Args.TransferID)

	step := workflows.DecidePollStep(polled.Error(), !polled.Failed() && polled.Value().Terminal(), time.Now(), 5*time.Minute)
	if step.SnoozeFor > 0 {
		return river.JobSnooze(step.SnoozeFor)
	}
	return step.Failure
}
