// Package pushes is the Component for domain pushes (DESIGN.md §3.1, M7):
// handing a domain to another DNSimple account without a registry transfer.
package pushes

import (
	"context"
	"log/slog"
	"regexp"

	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
	"dnsshimple/kit"
)

type Push struct {
	ID        int64
	DomainID  int64
	CreatedAt string
}

type InvalidPushError struct {
	Reason string
}

func (invalid InvalidPushError) Error() string {
	return "pushes: invalid push: " + invalid.Reason
}

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

type Gateway interface {
	InitiatePush(ctx context.Context, session identity.Session, domainName, newAccountEmail string) kit.Outcome[Push]
	Pushes(ctx context.Context, session identity.Session) kit.Outcome[[]Push]
	AcceptPush(ctx context.Context, session identity.Session, pushID, contactID int64) kit.Outcome[kit.Nothing]
	RejectPush(ctx context.Context, session identity.Session, pushID int64) kit.Outcome[kit.Nothing]
}

type Component struct {
	gateway Gateway
	metrics *telemetry.Metrics
}

func NewComponent(gateway Gateway, metrics *telemetry.Metrics) *Component {
	return &Component{gateway: gateway, metrics: metrics}
}

// InitiatePush is a sync BTX: once the other account accepts, the domain
// leaves this one.
func (component *Component) InitiatePush(ctx context.Context, session identity.Session, domainName, newAccountEmail string) kit.Outcome[Push] {
	if !emailPattern.MatchString(newAccountEmail) {
		return kit.Failure[Push](InvalidPushError{Reason: "the receiving account email is not an email address"})
	}
	ctx = telemetry.WithBTX(ctx, "PushDomain:"+domainName)
	pushed := component.gateway.InitiatePush(ctx, session, domainName, newAccountEmail)
	if pushed.Failed() {
		component.metrics.BTXOutcomes.WithLabelValues("PushDomain", "failed").Inc()
		slog.ErrorContext(ctx, "PushDomain failed", "cause", pushed.Error())
		return pushed
	}
	component.metrics.BTXOutcomes.WithLabelValues("PushDomain", "initiated").Inc()
	slog.InfoContext(ctx, "PushDomain initiated", "push_id", pushed.Value().ID)
	return pushed
}

func (component *Component) Pushes(ctx context.Context, session identity.Session) kit.Outcome[[]Push] {
	return component.gateway.Pushes(ctx, session)
}

func (component *Component) AcceptPush(ctx context.Context, session identity.Session, pushID, contactID int64) kit.Outcome[kit.Nothing] {
	accepted := component.gateway.AcceptPush(ctx, session, pushID, contactID)
	if !accepted.Failed() {
		slog.InfoContext(ctx, "push accepted", "push_id", pushID)
	}
	return accepted
}

func (component *Component) RejectPush(ctx context.Context, session identity.Session, pushID int64) kit.Outcome[kit.Nothing] {
	rejected := component.gateway.RejectPush(ctx, session, pushID)
	if !rejected.Failed() {
		slog.InfoContext(ctx, "push rejected", "push_id", pushID)
	}
	return rejected
}
