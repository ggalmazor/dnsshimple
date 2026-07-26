// Package identity is the Component that knows who is on the other end of an
// SSH connection: tenants, their SSH keys, their vault-sealed DNSimple
// tokens, and the link handshake that binds them together (DESIGN.md §4).
package identity

import (
	"context"
	"errors"
	"time"

	"dnsshimple/kit"
)

type Tenant struct {
	ID        int64
	AccountID int64 // the DNSimple account behind this tenant
	Email     string
}

// The Tenant as persistence knows it: token still sealed.
type TenantRecord struct {
	Tenant
	SealedToken []byte
}

// Session is what an authenticated SSH connection works with: the tenant and
// the opened token, held in memory only for the lifetime of the connection.
type Session struct {
	Tenant      Tenant
	AccessToken string
}

type PendingLink struct {
	Code        string
	Fingerprint string
	StartedAt   time.Time
}

// KeyBinding is one fingerprint-to-tenant attachment as the admin surface
// sees it: which key opens which account, and when it last did.
type KeyBinding struct {
	Fingerprint string
	Tenant      Tenant
	LastUsedAt  time.Time
}

// Census is the exact headcount of identity's tables - the admin
// dashboard's counter line, precise where table statistics only estimate.
type Census struct {
	Tenants      int64
	Bindings     int64
	PendingLinks int64
}

type RemoteIdentity struct {
	AccountID   int64
	Email       string
	AccessToken string
}

var (
	ErrUnknownFingerprint = errors.New("identity: this SSH key is not linked to any tenant")
	ErrUnknownTenant      = errors.New("identity: no tenant with this id")
	ErrLinkCodeUnknown    = errors.New("identity: the link code is unknown, expired, or already used")
)

type Repository interface {
	TenantBySSHKey(ctx context.Context, fingerprint string) kit.Outcome[TenantRecord]
	// TenantsBySSHKey lists every account the key opens, most recently
	// used first - the multi-account picker's source of truth.
	TenantsBySSHKey(ctx context.Context, fingerprint string) kit.Outcome[[]TenantRecord]
	TouchSSHKey(ctx context.Context, fingerprint string, tenantID int64) kit.Outcome[kit.Nothing]
	// UnlinkSSHKey removes one key-account binding; when it was the
	// tenant's last key, the tenant and its sealed token go with it.
	UnlinkSSHKey(ctx context.Context, fingerprint string, tenantID int64) kit.Outcome[kit.Nothing]
	// TenantByID serves callers without an SSH connection - background
	// workflows acting for a tenant long after the session is gone.
	TenantByID(ctx context.Context, tenantID int64) kit.Outcome[TenantRecord]
	SavePendingLink(ctx context.Context, link PendingLink) kit.Outcome[kit.Nothing]
	// PeekPendingLink answers whether the code is live without claiming it,
	// so the bridge can validate what the user typed before the redirect.
	PeekPendingLink(ctx context.Context, code string, notBefore time.Time) kit.Outcome[PendingLink]
	// ConsumePendingLink atomically claims the code: it either returns the
	// pending link started after notBefore, or ErrLinkCodeUnknown. A second
	// call with the same code must fail, whatever races it.
	ConsumePendingLink(ctx context.Context, code string, notBefore time.Time) kit.Outcome[PendingLink]
	// BindTenant upserts the tenant for this DNSimple account, stores the
	// sealed token, and attaches the SSH fingerprint to it.
	BindTenant(ctx context.Context, fingerprint string, remote RemoteIdentity, sealedToken []byte) kit.Outcome[Tenant]
	// AnnounceLink tells every process that the handshake for this code
	// landed, so the SSH session that issued it can wake wherever it lives.
	AnnounceLink(ctx context.Context, code, fingerprint string) kit.Outcome[kit.Nothing]
	// AwaitLink blocks until AnnounceLink fires for the code (or ctx ends)
	// and returns the tenant now bound to the fingerprint. Implementations
	// must also answer when the handshake landed before the wait began, so a
	// fast browser cannot out-race the parking SSH session.
	AwaitLink(ctx context.Context, code, fingerprint string) kit.Outcome[TenantRecord]
	// Bindings lists every key-account attachment, most recently used
	// first - the admin oversight view of who can open what.
	Bindings(ctx context.Context) kit.Outcome[[]KeyBinding]
	// DeletePendingLinksBefore removes link codes started before the
	// cutoff and reports how many went; consumption already ignores them,
	// this reclaims the rows.
	DeletePendingLinksBefore(ctx context.Context, cutoff time.Time) kit.Outcome[int64]
	// Census counts identity's rows exactly.
	Census(ctx context.Context) kit.Outcome[Census]
}

type TokenVault interface {
	Seal(accessToken string) kit.Outcome[[]byte]
	Open(sealedToken []byte) kit.Outcome[string]
}

// Authenticator is the DNSimple-facing port: it turns a browser grant into an
// asserted remote identity (token exchange plus whoami, one semantic step).
type Authenticator interface {
	AuthorizationPageURL(linkCode string) string
	Redeem(ctx context.Context, grantCode, linkCode string) kit.Outcome[RemoteIdentity]
}
