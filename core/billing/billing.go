// Package billing is the Component for account charges (DESIGN.md §3.1).
package billing

import (
	"context"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type Charge struct {
	InvoicedAt    string
	TotalAmount   string
	BalanceAmount string
	Reference     string
	State         string
}

type Gateway interface {
	Charges(ctx context.Context, session identity.Session) kit.Outcome[[]Charge]
}

type Component struct {
	gateway Gateway
}

func NewComponent(gateway Gateway) *Component {
	return &Component{gateway: gateway}
}

func (component *Component) Charges(ctx context.Context, session identity.Session) kit.Outcome[[]Charge] {
	return component.gateway.Charges(ctx, session)
}
