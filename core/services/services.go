// Package services is the Component for DNSimple's one-click services
// (DESIGN.md §3.1, M7): apply a provider's record bundle to a domain.
package services

import (
	"context"
	"log/slog"

	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
	"dnsshimple/kit"
)

type Service struct {
	ID          int64
	Name        string
	SID         string
	Description string
}

type Gateway interface {
	AvailableServices(ctx context.Context, session identity.Session) kit.Outcome[[]Service]
	AppliedServices(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]Service]
	ApplyService(ctx context.Context, session identity.Session, domainName string, serviceID int64) kit.Outcome[kit.Nothing]
	UnapplyService(ctx context.Context, session identity.Session, domainName string, serviceID int64) kit.Outcome[kit.Nothing]
}

type Component struct {
	gateway Gateway
	metrics *telemetry.Metrics
}

func NewComponent(gateway Gateway, metrics *telemetry.Metrics) *Component {
	return &Component{gateway: gateway, metrics: metrics}
}

func (component *Component) AvailableServices(ctx context.Context, session identity.Session) kit.Outcome[[]Service] {
	return component.gateway.AvailableServices(ctx, session)
}

func (component *Component) AppliedServices(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]Service] {
	return component.gateway.AppliedServices(ctx, session, domainName)
}

// ApplyService is a sync BTX: it writes the provider's records into the zone
// as one operation.
func (component *Component) ApplyService(ctx context.Context, session identity.Session, domainName string, serviceID int64) kit.Outcome[kit.Nothing] {
	ctx = telemetry.WithBTX(ctx, "ApplyService:"+domainName)
	applied := component.gateway.ApplyService(ctx, session, domainName, serviceID)
	if applied.Failed() {
		component.metrics.BTXOutcomes.WithLabelValues("ApplyService", "failed").Inc()
		slog.ErrorContext(ctx, "ApplyService failed", "cause", applied.Error())
		return applied
	}
	component.metrics.BTXOutcomes.WithLabelValues("ApplyService", "completed").Inc()
	slog.InfoContext(ctx, "ApplyService completed", "service_id", serviceID)
	return applied
}

func (component *Component) UnapplyService(ctx context.Context, session identity.Session, domainName string, serviceID int64) kit.Outcome[kit.Nothing] {
	return component.gateway.UnapplyService(ctx, session, domainName, serviceID)
}
