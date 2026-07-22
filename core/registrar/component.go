package registrar

import (
	"context"
	"log/slog"

	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
	"dnsshimple/kit"
)

type Component struct {
	gateway Gateway
	queue   PollQueue
	metrics *telemetry.Metrics
}

func NewComponent(gateway Gateway, queue PollQueue, metrics *telemetry.Metrics) *Component {
	return &Component{gateway: gateway, queue: queue, metrics: metrics}
}

func (component *Component) CheckDomain(ctx context.Context, session identity.Session, domainName string) kit.Outcome[Availability] {
	if err := ValidateDomainName(domainName); err != nil {
		return kit.Failure[Availability](err)
	}
	return component.gateway.CheckAvailability(ctx, session, domainName)
}

// RegisterDomain is the async BTX from DESIGN.md §3.2: check, register with
// the account's first contact, and hand the state polling to the queue. The
// caller gets the accepted Registration immediately; completion shows up in
// the domain list as DNSimple advances it.
func (component *Component) RegisterDomain(ctx context.Context, session identity.Session, domainName string) kit.Outcome[Registration] {
	if err := ValidateDomainName(domainName); err != nil {
		return kit.Failure[Registration](err)
	}
	ctx = telemetry.WithBTX(ctx, "RegisterDomain:"+domainName)

	available := kit.Chain(
		component.gateway.CheckAvailability(ctx, session, domainName),
		func(availability Availability) kit.Outcome[Availability] {
			if !availability.Available {
				return kit.Failure[Availability](ErrDomainTaken)
			}
			return kit.Success(availability)
		})
	registrant := kit.Chain(available, func(Availability) kit.Outcome[Contact] {
		return kit.Chain(
			component.gateway.Contacts(ctx, session),
			func(contacts []Contact) kit.Outcome[Contact] {
				if len(contacts) == 0 {
					return kit.Failure[Contact](ErrNoContacts)
				}
				return kit.Success(contacts[0])
			})
	})
	accepted := kit.Chain(registrant, func(contact Contact) kit.Outcome[Registration] {
		return component.gateway.Register(ctx, session, domainName, contact.ID)
	})
	queued := kit.Chain(accepted, func(registration Registration) kit.Outcome[Registration] {
		return kit.Chain(
			component.queue.EnqueueRegistrationPoll(ctx, session.Tenant.ID, domainName, registration.ID),
			func(kit.Nothing) kit.Outcome[Registration] { return kit.Success(registration) },
		)
	})

	if queued.Failed() {
		component.metrics.BTXOutcomes.WithLabelValues("RegisterDomain", "failed").Inc()
		slog.ErrorContext(ctx, "RegisterDomain failed", "cause", queued.Error())
		return queued
	}
	component.metrics.BTXOutcomes.WithLabelValues("RegisterDomain", "accepted").Inc()
	slog.InfoContext(ctx, "RegisterDomain accepted; polling queued",
		"registration_id", queued.Value().ID)
	return queued
}

func (component *Component) SetAutoRenewal(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing] {
	return component.gateway.SetAutoRenewal(ctx, session, domainName, enabled)
}

func (component *Component) WhoisPrivacy(ctx context.Context, session identity.Session, domainName string) kit.Outcome[bool] {
	return component.gateway.WhoisPrivacy(ctx, session, domainName)
}

func (component *Component) SetWhoisPrivacy(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing] {
	return component.gateway.SetWhoisPrivacy(ctx, session, domainName, enabled)
}

func (component *Component) TransferLock(ctx context.Context, session identity.Session, domainName string) kit.Outcome[bool] {
	return component.gateway.TransferLock(ctx, session, domainName)
}

func (component *Component) SetTransferLock(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing] {
	return component.gateway.SetTransferLock(ctx, session, domainName, enabled)
}

func (component *Component) Delegation(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]string] {
	return component.gateway.Delegation(ctx, session, domainName)
}

func (component *Component) SetDelegation(ctx context.Context, session identity.Session, domainName string, nameServers []string) kit.Outcome[kit.Nothing] {
	if err := ValidateDelegation(nameServers); err != nil {
		return kit.Failure[kit.Nothing](err)
	}
	return component.gateway.SetDelegation(ctx, session, domainName, nameServers)
}

// RenewDomain is a sync BTX (DESIGN.md §3.2): it spends money, so it counts
// and logs like every business operation.
func (component *Component) RenewDomain(ctx context.Context, session identity.Session, domainName string) kit.Outcome[Renewal] {
	ctx = telemetry.WithBTX(ctx, "RenewDomain:"+domainName)
	renewed := component.gateway.Renew(ctx, session, domainName)
	if renewed.Failed() {
		component.metrics.BTXOutcomes.WithLabelValues("RenewDomain", "failed").Inc()
		slog.ErrorContext(ctx, "RenewDomain failed", "cause", renewed.Error())
		return renewed
	}
	component.metrics.BTXOutcomes.WithLabelValues("RenewDomain", "accepted").Inc()
	slog.InfoContext(ctx, "RenewDomain accepted", "renewal_id", renewed.Value().ID)
	return renewed
}

// PollRegistration is the workflow step the job runner drives.
func (component *Component) PollRegistration(ctx context.Context, session identity.Session, domainName string, registrationID int64) kit.Outcome[Registration] {
	ctx = telemetry.WithBTX(ctx, "RegisterDomain:"+domainName)
	polled := component.gateway.RegistrationState(ctx, session, domainName, registrationID)
	if polled.Failed() {
		return polled
	}
	if polled.Value().Terminal() {
		outcome := "completed"
		if polled.Value().State != StateRegistered {
			outcome = "failed"
		}
		component.metrics.BTXOutcomes.WithLabelValues("RegisterDomain", outcome).Inc()
		slog.InfoContext(ctx, "RegisterDomain reached a terminal state", "state", polled.Value().State)
	}
	return polled
}
