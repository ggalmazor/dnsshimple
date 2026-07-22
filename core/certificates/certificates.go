// Package certificates is the Component for Let's Encrypt certificates
// (DESIGN.md §3.1). The sandbox does not support issuance, so this Component
// is grey-box tested against the spec fake and verified in production
// (decision recorded 2026-07-22).
package certificates

import (
	"context"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type Certificate struct {
	ID         int64
	CommonName string
	State      string
	AutoRenew  bool
	ExpiresAt  string
}

const (
	StateIssued    = "issued"
	StateFailed    = "failed"
	StateCancelled = "cancelled"
)

func (certificate Certificate) Terminal() bool {
	return certificate.State == StateIssued ||
		certificate.State == StateFailed ||
		certificate.State == StateCancelled
}

// Purchase is the first half of the Let's Encrypt flow; issuing turns it
// into a Certificate that then advances toward issued.
type Purchase struct {
	ID    int64
	State string
}

type Gateway interface {
	Certificates(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]Certificate]
	PurchaseLetsEncrypt(ctx context.Context, session identity.Session, domainName string, autoRenew bool) kit.Outcome[Purchase]
	IssueLetsEncrypt(ctx context.Context, session identity.Session, domainName string, purchaseID int64) kit.Outcome[Certificate]
	CertificateState(ctx context.Context, session identity.Session, domainName string, certificateID int64) kit.Outcome[Certificate]
}

type PollQueue interface {
	EnqueueCertificatePoll(ctx context.Context, tenantID int64, domainName string, certificateID int64) kit.Outcome[kit.Nothing]
}
