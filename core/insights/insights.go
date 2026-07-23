// Package insights is the Component for account insight reads (DESIGN.md
// §3.1, M8): DNS analytics and the registry's TLD catalog.
package insights

import (
	"context"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

// VolumeRow is one row of the DNS analytics report, grouped by zone.
type VolumeRow struct {
	ZoneName string
	Volume   int64
}

// TLD is one entry of the registry catalog with the flags the TUI shows.
type TLD struct {
	Name                string
	RegistrationEnabled bool
	TransferEnabled     bool
	WhoisPrivacy        bool
	IDN                 bool
	MinimumYears        int
}

type Gateway interface {
	VolumeByZone(ctx context.Context, session identity.Session) kit.Outcome[[]VolumeRow]
	TLDs(ctx context.Context, session identity.Session) kit.Outcome[[]TLD]
}

type Component struct {
	gateway Gateway
}

func NewComponent(gateway Gateway) *Component {
	return &Component{gateway: gateway}
}

func (component *Component) VolumeByZone(ctx context.Context, session identity.Session) kit.Outcome[[]VolumeRow] {
	return component.gateway.VolumeByZone(ctx, session)
}

func (component *Component) TLDs(ctx context.Context, session identity.Session) kit.Outcome[[]TLD] {
	return component.gateway.TLDs(ctx, session)
}
