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

// LinkedTenants lists every account this key opens, most recently used
// first; the current one leads.
func (component *Component) LinkedTenants(ctx context.Context, fingerprint string) kit.Outcome[[]Tenant] {
	return kit.Chain(
		component.repository.TenantsBySSHKey(ctx, fingerprint),
		func(records []TenantRecord) kit.Outcome[[]Tenant] {
			tenants := make([]Tenant, 0, len(records))
			for _, record := range records {
				tenants = append(tenants, record.Tenant)
			}
			return kit.Success(tenants)
		})
}

// SwitchAccount opens a session for another account the same key already
// links; a tenant the key never linked is refused, whatever the caller
// claims.
func (component *Component) SwitchAccount(ctx context.Context, fingerprint string, tenantID int64) kit.Outcome[Session] {
	return kit.Chain(
		component.repository.TenantsBySSHKey(ctx, fingerprint),
		func(records []TenantRecord) kit.Outcome[Session] {
			for _, record := range records {
				if record.ID != tenantID {
					continue
				}
				return kit.Chain(
					component.repository.TouchSSHKey(ctx, fingerprint, tenantID),
					func(kit.Nothing) kit.Outcome[Session] { return component.openSession(record) })
			}
			return kit.Failure[Session](ErrUnknownTenant)
		})
}

// Bindings is the admin oversight view: every key-account attachment,
// most recently used first.
func (component *Component) Bindings(ctx context.Context) kit.Outcome[[]KeyBinding] {
	return component.repository.Bindings(ctx)
}

// Census is the exact headcount of identity's data, for surfaces where
// the planner's estimates are not enough.
func (component *Component) Census(ctx context.Context) kit.Outcome[Census] {
	return component.repository.Census(ctx)
}

// PruneStaleLinks deletes link codes past their lifetime and reports how
// many went. Consumption already ignores them; abandoned handshakes only
// accumulate rows until this runs.
func (component *Component) PruneStaleLinks(ctx context.Context) kit.Outcome[int64] {
	return component.repository.DeletePendingLinksBefore(ctx, time.Now().Add(-LinkCodeLifetime))
}

// UnlinkAccount removes an account from this key. The caller keeps the
// session it already opened; the SSH adapter refuses to unlink the account
// a session is currently on, so a user always switches first.
func (component *Component) UnlinkAccount(ctx context.Context, fingerprint string, tenantID int64) kit.Outcome[kit.Nothing] {
	unlinked := component.repository.UnlinkSSHKey(ctx, fingerprint, tenantID)
	if !unlinked.Failed() {
		slog.InfoContext(ctx, "account unlinked", "tenant_id", tenantID)
	}
	return unlinked
}
