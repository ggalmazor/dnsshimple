// Package domains is the Component for the domain portfolio (DESIGN.md
// §3.1). Read paths for now; DNSSEC and email forwards grow here.
package domains

import (
	"context"

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

type Gateway interface {
	Domains(ctx context.Context, session identity.Session) kit.Outcome[[]Domain]
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
