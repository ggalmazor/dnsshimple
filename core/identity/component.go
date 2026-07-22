package identity

import (
	"context"
	"log/slog"
	"time"

	"dnsshimple/core/telemetry"
	"dnsshimple/kit"
)

// Component is the identity facade the driving adapters talk to.
type Component struct {
	repository Repository
	vault      TokenVault
	dnsimple   Authenticator
	metrics    *telemetry.Metrics
}

func NewComponent(repository Repository, vault TokenVault, dnsimple Authenticator, metrics *telemetry.Metrics) *Component {
	return &Component{repository: repository, vault: vault, dnsimple: dnsimple, metrics: metrics}
}

// SessionFor answers the SSH adapter's first question on connect: who is this
// key? ErrUnknownFingerprint means the link flow must run.
func (component *Component) SessionFor(ctx context.Context, fingerprint string) kit.Outcome[Session] {
	return kit.Chain(
		component.repository.TenantBySSHKey(ctx, fingerprint),
		component.openSession,
	)
}

// SessionForTenant is the background-workflow door: same opened Session,
// no SSH connection required.
func (component *Component) SessionForTenant(ctx context.Context, tenantID int64) kit.Outcome[Session] {
	return kit.Chain(
		component.repository.TenantByID(ctx, tenantID),
		component.openSession,
	)
}

// BeginLink issues and persists the code an unlinked session displays.
func (component *Component) BeginLink(ctx context.Context, fingerprint string) kit.Outcome[PendingLink] {
	link := NewPendingLink(fingerprint)
	return kit.Chain(
		component.repository.SavePendingLink(ctx, link),
		func(kit.Nothing) kit.Outcome[PendingLink] { return kit.Success(link) },
	)
}

// AuthorizationPageURL is where the bridge sends the user's browser for a code.
func (component *Component) AuthorizationPageURL(linkCode string) string {
	return component.dnsimple.AuthorizationPageURL(linkCode)
}

// HasPendingLink lets the bridge validate a typed code before redirecting.
func (component *Component) HasPendingLink(ctx context.Context, rawCode string) bool {
	peeked := component.repository.PeekPendingLink(ctx,
		NormalizeLinkCode(rawCode), time.Now().Add(-LinkCodeLifetime))
	return !peeked.Failed()
}

// CompleteLink is the LinkAccount Business Transaction: consume the code,
// redeem the grant with DNSimple, seal the token, bind the tenant, and
// announce the result so the waiting SSH session wakes.
func (component *Component) CompleteLink(ctx context.Context, rawCode, grantCode string) kit.Outcome[Tenant] {
	linkCode := NormalizeLinkCode(rawCode)
	ctx = telemetry.WithBTX(ctx, "LinkAccount:"+linkCode)

	consumed := component.repository.ConsumePendingLink(ctx,
		linkCode, time.Now().Add(-LinkCodeLifetime))
	bound := kit.Chain(consumed, func(link PendingLink) kit.Outcome[boundIdentity] {
		return component.redeemAndSeal(ctx, link, grantCode)
	})
	tenant := kit.Chain(bound, func(sealed boundIdentity) kit.Outcome[Tenant] {
		return component.repository.BindTenant(ctx, sealed.fingerprint, sealed.remote, sealed.sealedToken)
	})
	announced := kit.Chain(tenant, func(linkedTenant Tenant) kit.Outcome[Tenant] {
		// bound is guaranteed successful here: tenant chained from it.
		return kit.Chain(
			component.repository.AnnounceLink(ctx, linkCode, bound.Value().fingerprint),
			func(kit.Nothing) kit.Outcome[Tenant] { return kit.Success(linkedTenant) },
		)
	})

	if announced.Failed() {
		component.metrics.BTXOutcomes.WithLabelValues("LinkAccount", "failed").Inc()
		slog.ErrorContext(ctx, "LinkAccount failed", "cause", announced.Error())
		return announced
	}
	component.metrics.BTXOutcomes.WithLabelValues("LinkAccount", "completed").Inc()
	slog.InfoContext(ctx, "LinkAccount completed",
		"account_id", announced.Value().AccountID, "tenant_id", announced.Value().ID)
	return announced
}

// AwaitLink parks the SSH session until its handshake lands, then hands it a
// ready Session.
func (component *Component) AwaitLink(ctx context.Context, code, fingerprint string) kit.Outcome[Session] {
	return kit.Chain(
		component.repository.AwaitLink(ctx, code, fingerprint),
		component.openSession,
	)
}

// --- internal steps -------------------------------------------------------

type boundIdentity struct {
	fingerprint string
	remote      RemoteIdentity
	sealedToken []byte
}

func (component *Component) redeemAndSeal(ctx context.Context, link PendingLink, grantCode string) kit.Outcome[boundIdentity] {
	return kit.Chain(
		component.dnsimple.Redeem(ctx, grantCode, link.Code),
		func(remote RemoteIdentity) kit.Outcome[boundIdentity] {
			return kit.Chain(
				component.vault.Seal(remote.AccessToken),
				func(sealed []byte) kit.Outcome[boundIdentity] {
					return kit.Success(boundIdentity{
						fingerprint: link.Fingerprint,
						remote:      remote,
						sealedToken: sealed,
					})
				})
		})
}

func (component *Component) openSession(record TenantRecord) kit.Outcome[Session] {
	return kit.Chain(
		component.vault.Open(record.SealedToken),
		func(accessToken string) kit.Outcome[Session] {
			return kit.Success(Session{Tenant: record.Tenant, AccessToken: accessToken})
		})
}
