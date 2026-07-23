package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"dnsshimple/core/certificates"
	"dnsshimple/core/registrar"
	"dnsshimple/kit"
)

// Queue implements the registrar PollQueue port on River. Bind happens after
// river.NewClient because the client needs the workers first, and the workers
// need the Components that carry this queue - Bind breaks the cycle.
type Queue struct {
	client *river.Client[pgx.Tx]
}

func (queue *Queue) Bind(client *river.Client[pgx.Tx]) {
	queue.client = client
}

var (
	_ registrar.PollQueue    = (*Queue)(nil)
	_ certificates.PollQueue = (*Queue)(nil)
)

func (queue *Queue) EnqueueRegistrationPoll(ctx context.Context, tenantID int64, domainName string, registrationID int64) kit.Outcome[kit.Nothing] {
	if queue.client == nil {
		return kit.Failure[kit.Nothing](errors.New("jobs: the queue is not bound to a running client"))
	}
	_, err := queue.client.Insert(ctx, RegistrationPollArgs{
		TenantID:       tenantID,
		DomainName:     domainName,
		RegistrationID: registrationID,
	}, &river.InsertOpts{
		// Registrations take a moment to move; the first look can wait.
		ScheduledAt: time.Now().Add(10 * time.Second),
	})
	if err != nil {
		return kit.Failure[kit.Nothing](fmt.Errorf("queueing the registration poll: %w", err))
	}
	return kit.Done()
}

func (queue *Queue) EnqueueTransferPoll(ctx context.Context, tenantID int64, domainName string, transferID int64) kit.Outcome[kit.Nothing] {
	if queue.client == nil {
		return kit.Failure[kit.Nothing](errors.New("jobs: the queue is not bound to a running client"))
	}
	_, err := queue.client.Insert(ctx, TransferPollArgs{
		TenantID:   tenantID,
		DomainName: domainName,
		TransferID: transferID,
	}, &river.InsertOpts{
		// Registry transfers move on the scale of minutes to days; the
		// first look can comfortably wait.
		ScheduledAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		return kit.Failure[kit.Nothing](fmt.Errorf("queueing the transfer poll: %w", err))
	}
	return kit.Done()
}

func (queue *Queue) EnqueueRegistrantChangePoll(ctx context.Context, tenantID, changeID int64) kit.Outcome[kit.Nothing] {
	if queue.client == nil {
		return kit.Failure[kit.Nothing](errors.New("jobs: the queue is not bound to a running client"))
	}
	_, err := queue.client.Insert(ctx, RegistrantChangePollArgs{TenantID: tenantID, ChangeID: changeID},
		&river.InsertOpts{ScheduledAt: time.Now().Add(time.Minute)})
	if err != nil {
		return kit.Failure[kit.Nothing](fmt.Errorf("queueing the registrant change poll: %w", err))
	}
	return kit.Done()
}

func (queue *Queue) EnqueueCertificatePoll(ctx context.Context, tenantID int64, domainName string, certificateID int64) kit.Outcome[kit.Nothing] {
	if queue.client == nil {
		return kit.Failure[kit.Nothing](errors.New("jobs: the queue is not bound to a running client"))
	}
	_, err := queue.client.Insert(ctx, CertificatePollArgs{
		TenantID:      tenantID,
		DomainName:    domainName,
		CertificateID: certificateID,
	}, &river.InsertOpts{ScheduledAt: time.Now().Add(10 * time.Second)})
	if err != nil {
		return kit.Failure[kit.Nothing](fmt.Errorf("queueing the certificate poll: %w", err))
	}
	return kit.Done()
}
