// Package secondarydns is the Component for secondary DNS (DESIGN.md §3.1,
// M8): primary servers DNSimple slaves zones from.
package secondarydns

import (
	"context"
	"net"
	"strings"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type PrimaryServer struct {
	ID     int64
	Name   string
	IP     string
	Port   int
	Linked bool
}

type InvalidPrimaryError struct {
	Reason string
}

func (invalid InvalidPrimaryError) Error() string {
	return "secondarydns: invalid primary server: " + invalid.Reason
}

type Gateway interface {
	Primaries(ctx context.Context, session identity.Session) kit.Outcome[[]PrimaryServer]
	CreatePrimary(ctx context.Context, session identity.Session, name, ip string, port int) kit.Outcome[PrimaryServer]
	DeletePrimary(ctx context.Context, session identity.Session, primaryID int64) kit.Outcome[kit.Nothing]
	SetPrimaryLink(ctx context.Context, session identity.Session, primaryID int64, linked bool) kit.Outcome[kit.Nothing]
	SecondaryZones(ctx context.Context, session identity.Session) kit.Outcome[[]string]
	CreateSecondaryZone(ctx context.Context, session identity.Session, zoneName string) kit.Outcome[kit.Nothing]
}

type Component struct {
	gateway Gateway
}

func NewComponent(gateway Gateway) *Component {
	return &Component{gateway: gateway}
}

func (component *Component) Primaries(ctx context.Context, session identity.Session) kit.Outcome[[]PrimaryServer] {
	return component.gateway.Primaries(ctx, session)
}

func (component *Component) CreatePrimary(ctx context.Context, session identity.Session, name, ip string, port int) kit.Outcome[PrimaryServer] {
	name = strings.TrimSpace(name)
	ip = strings.TrimSpace(ip)
	if name == "" {
		return kit.Failure[PrimaryServer](InvalidPrimaryError{Reason: "the name must not be empty"})
	}
	if net.ParseIP(ip) == nil {
		return kit.Failure[PrimaryServer](InvalidPrimaryError{Reason: "the IP must be a valid IPv4 or IPv6 address"})
	}
	if port <= 0 || port > 65535 {
		return kit.Failure[PrimaryServer](InvalidPrimaryError{Reason: "the port must be between 1 and 65535"})
	}
	return component.gateway.CreatePrimary(ctx, session, name, ip, port)
}

func (component *Component) DeletePrimary(ctx context.Context, session identity.Session, primaryID int64) kit.Outcome[kit.Nothing] {
	return component.gateway.DeletePrimary(ctx, session, primaryID)
}

func (component *Component) SetPrimaryLink(ctx context.Context, session identity.Session, primaryID int64, linked bool) kit.Outcome[kit.Nothing] {
	return component.gateway.SetPrimaryLink(ctx, session, primaryID, linked)
}

func (component *Component) SecondaryZones(ctx context.Context, session identity.Session) kit.Outcome[[]string] {
	return component.gateway.SecondaryZones(ctx, session)
}

func (component *Component) CreateSecondaryZone(ctx context.Context, session identity.Session, zoneName string) kit.Outcome[kit.Nothing] {
	if strings.TrimSpace(zoneName) == "" {
		return kit.Failure[kit.Nothing](InvalidPrimaryError{Reason: "the zone name must not be empty"})
	}
	return component.gateway.CreateSecondaryZone(ctx, session, strings.ToLower(strings.TrimSpace(zoneName)))
}
