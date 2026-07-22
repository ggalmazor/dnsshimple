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

// Tenant is a linked DNSimple account as the rest of the system sees it.
type Tenant struct {
	ID        int64
	AccountID int64 // the DNSimple account behind this tenant
	Email     string
}

// TenantRecord is the Tenant as persistence knows it: token still sealed.
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

// PendingLink is one in-flight browser handshake.
type PendingLink struct {
	Code        string
	Fingerprint string
	StartedAt   time.Time
}

// RemoteIdentity is what DNSimple asserts about a freshly redeemed token.
type RemoteIdentity struct {
	AccountID   int64
	Email       string
	AccessToken string
}

// Domain failures the driving adapters branch on.
var (
	ErrUnknownFingerprint = errors.New("identity: this SSH key is not linked to any tenant")
	ErrLinkCodeUnknown    = errors.New("identity: the link code is unknown, expired, or already used")
)

// Repository is the persistence port of the Component.
type Repository interface {
	TenantBySSHKey(ctx context.Context, fingerprint string) kit.Outcome[TenantRecord]
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
	// and returns the tenant now bound to the handshake's fingerprint.
	AwaitLink(ctx context.Context, code string) kit.Outcome[TenantRecord]
}

// TokenVault seals and opens DNSimple access tokens at rest.
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
