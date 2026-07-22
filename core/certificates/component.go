package certificates

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

func (component *Component) Certificates(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]Certificate] {
	return component.gateway.Certificates(ctx, session, domainName)
}

// IssueCertificate is the async BTX from DESIGN.md §3.2: purchase the Let's
// Encrypt order, request issuance, and hand state polling to the queue.
func (component *Component) IssueCertificate(ctx context.Context, session identity.Session, domainName string, request IssueRequest) kit.Outcome[Certificate] {
	ctx = telemetry.WithBTX(ctx, "IssueCertificate:"+domainName)

	requested := kit.Chain(
		component.gateway.PurchaseLetsEncrypt(ctx, session, domainName, request),
		func(purchase Purchase) kit.Outcome[Certificate] {
			return component.gateway.IssueLetsEncrypt(ctx, session, domainName, purchase.ID)
		})
	queued := kit.Chain(requested, func(certificate Certificate) kit.Outcome[Certificate] {
		return kit.Chain(
			component.queue.EnqueueCertificatePoll(ctx, session.Tenant.ID, domainName, certificate.ID),
			func(kit.Nothing) kit.Outcome[Certificate] { return kit.Success(certificate) },
		)
	})

	if queued.Failed() {
		component.metrics.BTXOutcomes.WithLabelValues("IssueCertificate", "failed").Inc()
		slog.ErrorContext(ctx, "IssueCertificate failed", "cause", queued.Error())
		return queued
	}
	component.metrics.BTXOutcomes.WithLabelValues("IssueCertificate", "accepted").Inc()
	slog.InfoContext(ctx, "IssueCertificate accepted; polling queued",
		"certificate_id", queued.Value().ID)
	return queued
}

// PollCertificate is the workflow step the job runner drives.
func (component *Component) PollCertificate(ctx context.Context, session identity.Session, domainName string, certificateID int64) kit.Outcome[Certificate] {
	ctx = telemetry.WithBTX(ctx, "IssueCertificate:"+domainName)
	polled := component.gateway.CertificateState(ctx, session, domainName, certificateID)
	if polled.Failed() {
		return polled
	}
	if polled.Value().Terminal() {
		outcome := "completed"
		if polled.Value().State != StateIssued {
			outcome = "failed"
		}
		component.metrics.BTXOutcomes.WithLabelValues("IssueCertificate", outcome).Inc()
		slog.InfoContext(ctx, "IssueCertificate reached a terminal state", "state", polled.Value().State)
	}
	return polled
}
