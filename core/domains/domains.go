// Package domains is the Component for the domain portfolio (DESIGN.md
// §3.1). Read paths for now; DNSSEC and email forwards grow here.
package domains

import (
	"context"
	"strings"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type Domain struct {
	ID        int64
	Name      string
	State     string
	AutoRenew bool
	ExpiresAt string
}

type DNSSECStatus struct {
	Enabled bool
}

type EmailForward struct {
	ID   int64
	From string
	To   string
}

type InvalidForwardError struct {
	Reason string
}

func (invalid InvalidForwardError) Error() string {
	return "domains: invalid email forward: " + invalid.Reason
}

type Gateway interface {
	Domains(ctx context.Context, session identity.Session) kit.Outcome[[]Domain]
	DNSSEC(ctx context.Context, session identity.Session, domainName string) kit.Outcome[DNSSECStatus]
	SetDNSSEC(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing]
	EmailForwards(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]EmailForward]
	CreateEmailForward(ctx context.Context, session identity.Session, domainName, alias, destination string) kit.Outcome[EmailForward]
	DeleteEmailForward(ctx context.Context, session identity.Session, domainName string, forwardID int64) kit.Outcome[kit.Nothing]
}

type Component struct {
	gateway Gateway
}

func NewComponent(gateway Gateway) *Component {
	return &Component{gateway: gateway}
}

func (component *Component) Domains(ctx context.Context, session identity.Session) kit.Outcome[[]Domain] {
	return component.gateway.Domains(ctx, session)
}

func (component *Component) DNSSEC(ctx context.Context, session identity.Session, domainName string) kit.Outcome[DNSSECStatus] {
	return component.gateway.DNSSEC(ctx, session, domainName)
}

func (component *Component) SetDNSSEC(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing] {
	return component.gateway.SetDNSSEC(ctx, session, domainName, enabled)
}

func (component *Component) EmailForwards(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]EmailForward] {
	return component.gateway.EmailForwards(ctx, session, domainName)
}

// CreateEmailForward rejects drafts DNSimple would reject before a request
// is spent, mirroring the zones validation stance.
func (component *Component) CreateEmailForward(ctx context.Context, session identity.Session, domainName, alias, destination string) kit.Outcome[EmailForward] {
	if strings.TrimSpace(alias) == "" {
		return kit.Failure[EmailForward](InvalidForwardError{Reason: "the alias must not be empty"})
	}
	if !strings.Contains(destination, "@") {
		return kit.Failure[EmailForward](InvalidForwardError{Reason: "the destination must be an email address"})
	}
	return component.gateway.CreateEmailForward(ctx, session, domainName, strings.TrimSpace(alias), strings.TrimSpace(destination))
}

func (component *Component) DeleteEmailForward(ctx context.Context, session identity.Session, domainName string, forwardID int64) kit.Outcome[kit.Nothing] {
	return component.gateway.DeleteEmailForward(ctx, session, domainName, forwardID)
}
