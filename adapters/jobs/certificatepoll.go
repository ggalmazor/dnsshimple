package jobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"dnsshimple/core/certificates"
	"dnsshimple/core/identity"
)

type CertificatePollArgs struct {
	TenantID      int64  `json:"tenant_id"`
	DomainName    string `json:"domain_name"`
	CertificateID int64  `json:"certificate_id"`
}

func (CertificatePollArgs) Kind() string { return "certificate_poll" }

type CertificatePollWorker struct {
	river.WorkerDefaults[CertificatePollArgs]
	Identity     *identity.Component
	Certificates *certificates.Component
}

func (worker *CertificatePollWorker) Work(ctx context.Context, job *river.Job[CertificatePollArgs]) error {
	session := worker.Identity.SessionForTenant(ctx, job.Args.TenantID)
	if session.Failed() {
		return session.Error()
	}
	polled := worker.Certificates.PollCertificate(ctx,
		session.Value(), job.Args.DomainName, job.Args.CertificateID)

	step := decidePollStep(polled.Error(), !polled.Failed() && polled.Value().Terminal(), time.Now(), 15*time.Second)
	if step.snoozeFor > 0 {
		return river.JobSnooze(step.snoozeFor)
	}
	return step.failure
}
