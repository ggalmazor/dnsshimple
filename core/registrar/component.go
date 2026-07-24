package registrar

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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
func (component *Component) RegisterDomain(ctx context.Context, session identity.Session, domainName string, extendedAttributes map[string]string) kit.Outcome[Registration] {
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
		if err := component.demandRefusal(ctx, session, domainName, extendedAttributes); err != nil {
			return kit.Failure[Contact](err)
		}
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
		return component.gateway.Register(ctx, session, domainName, contact.ID, extendedAttributes)
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

// TransferDomain is the async BTX for inbound transfers: validate, submit
// with the account's first contact and the losing registrar's auth code,
// and hand state polling to the queue.
func (component *Component) TransferDomain(ctx context.Context, session identity.Session, domainName, authCode string, extendedAttributes map[string]string) kit.Outcome[Transfer] {
	if err := ValidateDomainName(domainName); err != nil {
		return kit.Failure[Transfer](err)
	}
	if strings.TrimSpace(authCode) == "" {
		return kit.Failure[Transfer](InvalidDomainError{Reason: "the transfer needs the auth code from the losing registrar"})
	}
	ctx = telemetry.WithBTX(ctx, "TransferDomain:"+domainName)
	if err := component.demandRefusal(ctx, session, domainName, extendedAttributes); err != nil {
		return kit.Failure[Transfer](err)
	}

	registrant := kit.Chain(
		component.gateway.Contacts(ctx, session),
		func(contacts []Contact) kit.Outcome[Contact] {
			if len(contacts) == 0 {
				return kit.Failure[Contact](ErrNoContacts)
			}
			return kit.Success(contacts[0])
		})
	accepted := kit.Chain(registrant, func(contact Contact) kit.Outcome[Transfer] {
		return component.gateway.Transfer(ctx, session, domainName, contact.ID, strings.TrimSpace(authCode), extendedAttributes)
	})
	queued := kit.Chain(accepted, func(transfer Transfer) kit.Outcome[Transfer] {
		return kit.Chain(
			component.queue.EnqueueTransferPoll(ctx, session.Tenant.ID, domainName, transfer.ID),
			func(kit.Nothing) kit.Outcome[Transfer] { return kit.Success(transfer) },
		)
	})

	if queued.Failed() {
		component.metrics.BTXOutcomes.WithLabelValues("TransferDomain", "failed").Inc()
		slog.ErrorContext(ctx, "TransferDomain failed", "cause", queued.Error())
		return queued
	}
	component.metrics.BTXOutcomes.WithLabelValues("TransferDomain", "accepted").Inc()
	slog.InfoContext(ctx, "TransferDomain accepted; polling queued", "transfer_id", queued.Value().ID)
	return queued
}

// PollTransfer is the workflow step the job runner drives.
func (component *Component) PollTransfer(ctx context.Context, session identity.Session, domainName string, transferID int64) kit.Outcome[Transfer] {
	ctx = telemetry.WithBTX(ctx, "TransferDomain:"+domainName)
	polled := component.gateway.TransferState(ctx, session, domainName, transferID)
	if polled.Failed() {
		return polled
	}
	if polled.Value().Terminal() {
		outcome := "completed"
		if polled.Value().State != TransferCompleted {
			outcome = "failed"
		}
		component.metrics.BTXOutcomes.WithLabelValues("TransferDomain", outcome).Inc()
		slog.InfoContext(ctx, "TransferDomain reached a terminal state", "state", polled.Value().State)
	}
	return polled
}

// AuthorizeTransferOut asks DNSimple to release the auth code (sent to the
// registrant by email); the domain then transfers away at the gaining side.
func (component *Component) AuthorizeTransferOut(ctx context.Context, session identity.Session, domainName string) kit.Outcome[kit.Nothing] {
	ctx = telemetry.WithBTX(ctx, "AuthorizeTransferOut:"+domainName)
	authorized := component.gateway.AuthorizeTransferOut(ctx, session, domainName)
	if authorized.Failed() {
		component.metrics.BTXOutcomes.WithLabelValues("AuthorizeTransferOut", "failed").Inc()
		slog.ErrorContext(ctx, "AuthorizeTransferOut failed", "cause", authorized.Error())
		return authorized
	}
	component.metrics.BTXOutcomes.WithLabelValues("AuthorizeTransferOut", "completed").Inc()
	slog.InfoContext(ctx, "AuthorizeTransferOut completed")
	return authorized
}

// RestoreDomain orders a restore for an expired domain - it charges the
// account, so it counts like every business operation.
func (component *Component) RestoreDomain(ctx context.Context, session identity.Session, domainName string) kit.Outcome[Restore] {
	ctx = telemetry.WithBTX(ctx, "RestoreDomain:"+domainName)
	restored := component.gateway.Restore(ctx, session, domainName)
	if restored.Failed() {
		component.metrics.BTXOutcomes.WithLabelValues("RestoreDomain", "failed").Inc()
		slog.ErrorContext(ctx, "RestoreDomain failed", "cause", restored.Error())
		return restored
	}
	component.metrics.BTXOutcomes.WithLabelValues("RestoreDomain", "accepted").Inc()
	slog.InfoContext(ctx, "RestoreDomain accepted", "restore_id", restored.Value().ID)
	return restored
}

// ChangeRegistrant is the async BTX moving a domain to another contact:
// check what the registry demands, refuse politely when it demands data the
// TUI cannot collect, then create the change and queue the state poll.
func (component *Component) ChangeRegistrant(ctx context.Context, session identity.Session, domainID, contactID int64) kit.Outcome[RegistrantChange] {
	ctx = telemetry.WithBTX(ctx, fmt.Sprintf("ChangeRegistrant:%d", domainID))

	checked := kit.Chain(
		component.gateway.CheckRegistrantChange(ctx, session, domainID, contactID),
		func(check RegistrantChangeCheck) kit.Outcome[RegistrantChangeCheck] {
			if check.RequiresExtendedAttributes {
				return kit.Failure[RegistrantChangeCheck](ErrNeedsExtendedAttributes)
			}
			return kit.Success(check)
		})
	created := kit.Chain(checked, func(RegistrantChangeCheck) kit.Outcome[RegistrantChange] {
		return component.gateway.CreateRegistrantChange(ctx, session, domainID, contactID)
	})
	queued := kit.Chain(created, func(change RegistrantChange) kit.Outcome[RegistrantChange] {
		if change.Terminal() {
			return kit.Success(change) // some registries complete instantly
		}
		return kit.Chain(
			component.queue.EnqueueRegistrantChangePoll(ctx, session.Tenant.ID, change.ID),
			func(kit.Nothing) kit.Outcome[RegistrantChange] { return kit.Success(change) },
		)
	})

	if queued.Failed() {
		component.metrics.BTXOutcomes.WithLabelValues("ChangeRegistrant", "failed").Inc()
		slog.ErrorContext(ctx, "ChangeRegistrant failed", "cause", queued.Error())
		return queued
	}
	component.metrics.BTXOutcomes.WithLabelValues("ChangeRegistrant", "accepted").Inc()
	slog.InfoContext(ctx, "ChangeRegistrant accepted", "change_id", queued.Value().ID)
	return queued
}

// PollRegistrantChange is the workflow step the job runner drives.
func (component *Component) PollRegistrantChange(ctx context.Context, session identity.Session, changeID int64) kit.Outcome[RegistrantChange] {
	ctx = telemetry.WithBTX(ctx, fmt.Sprintf("ChangeRegistrant:change-%d", changeID))
	polled := component.gateway.RegistrantChangeState(ctx, session, changeID)
	if polled.Failed() {
		return polled
	}
	if polled.Value().Terminal() {
		outcome := "completed"
		if polled.Value().State != RegistrantChangeCompleted {
			outcome = "failed"
		}
		component.metrics.BTXOutcomes.WithLabelValues("ChangeRegistrant", outcome).Inc()
		slog.InfoContext(ctx, "ChangeRegistrant reached a terminal state", "state", polled.Value().State)
	}
	return polled
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

// QuoteDomain answers the register form's real question: is it available,
// and what does it cost - before anything is committed.
func (component *Component) QuoteDomain(ctx context.Context, session identity.Session, domainName string) kit.Outcome[Quote] {
	if err := ValidateDomainName(domainName); err != nil {
		return kit.Failure[Quote](err)
	}
	return kit.Chain(
		component.gateway.CheckAvailability(ctx, session, domainName),
		func(availability Availability) kit.Outcome[Quote] {
			if !availability.Available {
				return kit.Success(Quote{Available: false, Premium: availability.Premium})
			}
			return kit.Chain(
				component.gateway.Prices(ctx, session, domainName),
				func(price float64) kit.Outcome[Quote] {
					return kit.Success(Quote{Available: true, Premium: availability.Premium, RegistrationPrice: price})
				})
		})
}

// Price answers what registering a name under a TLD costs, without the
// availability half of QuoteDomain - the TLD catalog uses it.
func (component *Component) Price(ctx context.Context, session identity.Session, domainName string) kit.Outcome[float64] {
	return component.gateway.Prices(ctx, session, domainName)
}

// SetVanityNameServers enables or disables vanity name servers (ns1.<domain>
// and friends) for the domain; DNSimple exposes no read for the state, so
// both directions are explicit actions.
func (component *Component) SetVanityNameServers(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing] {
	return component.gateway.SetVanityNameServers(ctx, session, domainName, enabled)
}

func (component *Component) RegistrantChanges(ctx context.Context, session identity.Session) kit.Outcome[[]RegistrantChange] {
	return component.gateway.RegistrantChanges(ctx, session)
}

// CancelRegistrantChange withdraws an in-flight change; terminal ones are
// refused by DNSimple and its message passes through.
func (component *Component) CancelRegistrantChange(ctx context.Context, session identity.Session, changeID int64) kit.Outcome[kit.Nothing] {
	cancelled := component.gateway.CancelRegistrantChange(ctx, session, changeID)
	if !cancelled.Failed() {
		slog.InfoContext(ctx, "registrant change cancelled", "change_id", changeID)
	}
	return cancelled
}

// CancelTransfer withdraws an in-flight inbound transfer; the poll worker
// observes the cancelled state and ends the workflow.
func (component *Component) CancelTransfer(ctx context.Context, session identity.Session, domainName string, transferID int64) kit.Outcome[kit.Nothing] {
	cancelled := component.gateway.CancelTransfer(ctx, session, domainName, transferID)
	if !cancelled.Failed() {
		slog.InfoContext(ctx, "transfer cancelled", "transfer_id", transferID)
	}
	return cancelled
}

// DelegateToVanity points the domain's delegation at its vanity name
// servers; DNSimple answers 412 when vanity name servers are not enabled.
func (component *Component) DelegateToVanity(ctx context.Context, session identity.Session, domainName string, nameServers []string) kit.Outcome[kit.Nothing] {
	delegated := component.gateway.DelegateToVanity(ctx, session, domainName, nameServers)
	if !delegated.Failed() {
		slog.InfoContext(ctx, "delegated to vanity name servers", "domain", domainName)
	}
	return delegated
}

func (component *Component) DedelegateFromVanity(ctx context.Context, session identity.Session, domainName string) kit.Outcome[kit.Nothing] {
	dedelegated := component.gateway.DedelegateFromVanity(ctx, session, domainName)
	if !dedelegated.Failed() {
		slog.InfoContext(ctx, "dedelegated from vanity name servers", "domain", domainName)
	}
	return dedelegated
}

func (component *Component) TLDs(ctx context.Context, session identity.Session) kit.Outcome[[]TLD] {
	return component.gateway.TLDs(ctx, session)
}

func (component *Component) ExtendedAttributes(ctx context.Context, session identity.Session, tld string) kit.Outcome[[]ExtendedAttribute] {
	return component.gateway.ExtendedAttributes(ctx, session, tld)
}

// demandRefusal asks the registry what the write needs and refuses with the
// form definition when a required field is missing; a registry lookup
// failure deliberately falls through to the write, whose upstream error
// names what the registry wanted.
func (component *Component) demandRefusal(ctx context.Context, session identity.Session, domainName string, supplied map[string]string) error {
	dot := strings.Index(domainName, ".")
	if dot < 0 {
		return nil
	}
	demands := component.gateway.ExtendedAttributes(ctx, session, domainName[dot+1:])
	if demands.Failed() {
		return nil
	}
	for _, attribute := range demands.Value() {
		if attribute.Required && supplied[attribute.Name] == "" {
			return NeedsExtendedAttributesError{Demands: demands.Value()}
		}
	}
	return nil
}
