// Package domains is the Component for the domain portfolio (DESIGN.md
// §3.1). Read paths for now; DNSSEC and email forwards grow here.
package domains

import (
	"context"
	"log/slog"
	"strings"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type Domain struct {
	ID        int64
	Name      string
	State     string
	AutoRenew bool
	ExpiresAt string
}

type DNSSECStatus struct {
	Enabled bool
}

type EmailForward struct {
	ID   int64
	From string
	To   string
}

type InvalidForwardError struct {
	Reason string
}

func (invalid InvalidForwardError) Error() string {
	return "domains: invalid email forward: " + invalid.Reason
}

type InvalidDomainNameError struct {
	Reason string
}

func (invalid InvalidDomainNameError) Error() string {
	return "domains: invalid domain name: " + invalid.Reason
}

// ValidateDomainName rejects obvious non-names before the API call; the
// registry stays the authority on everything subtler.
func ValidateDomainName(name string) error {
	if name == "" {
		return InvalidDomainNameError{Reason: "the domain name must not be empty"}
	}
	if len(name) > 253 {
		return InvalidDomainNameError{Reason: "the domain name exceeds 253 characters"}
	}
	if !strings.Contains(name, ".") {
		return InvalidDomainNameError{Reason: "the domain name needs at least one dot"}
	}
	if strings.ContainsAny(name, " \t") {
		return InvalidDomainNameError{Reason: "the domain name must not contain spaces"}
	}
	return nil
}

type Gateway interface {
	Domains(ctx context.Context, session identity.Session) kit.Outcome[[]Domain]
	AddDomain(ctx context.Context, session identity.Session, domainName string) kit.Outcome[Domain]
	DeleteDomain(ctx context.Context, session identity.Session, domainName string) kit.Outcome[kit.Nothing]
	DNSSEC(ctx context.Context, session identity.Session, domainName string) kit.Outcome[DNSSECStatus]
	SetDNSSEC(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing]
	EmailForwards(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]EmailForward]
	CreateEmailForward(ctx context.Context, session identity.Session, domainName, alias, destination string) kit.Outcome[EmailForward]
	DeleteEmailForward(ctx context.Context, session identity.Session, domainName string, forwardID int64) kit.Outcome[kit.Nothing]
	DSRecords(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]DSRecord]
	CreateDSRecord(ctx context.Context, session identity.Session, domainName string, record DSRecord) kit.Outcome[DSRecord]
	DeleteDSRecord(ctx context.Context, session identity.Session, domainName string, dsRecordID int64) kit.Outcome[kit.Nothing]
}

type Component struct {
	gateway Gateway
}

func NewComponent(gateway Gateway) *Component {
	return &Component{gateway: gateway}
}

func (component *Component) Domains(ctx context.Context, session identity.Session) kit.Outcome[[]Domain] {
	return component.gateway.Domains(ctx, session)
}

func (component *Component) DNSSEC(ctx context.Context, session identity.Session, domainName string) kit.Outcome[DNSSECStatus] {
	return component.gateway.DNSSEC(ctx, session, domainName)
}

func (component *Component) SetDNSSEC(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing] {
	return component.gateway.SetDNSSEC(ctx, session, domainName, enabled)
}

func (component *Component) EmailForwards(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]EmailForward] {
	return component.gateway.EmailForwards(ctx, session, domainName)
}

// CreateEmailForward rejects drafts DNSimple would reject before a request
// is spent, mirroring the zones validation stance.
func (component *Component) CreateEmailForward(ctx context.Context, session identity.Session, domainName, alias, destination string) kit.Outcome[EmailForward] {
	if strings.TrimSpace(alias) == "" {
		return kit.Failure[EmailForward](InvalidForwardError{Reason: "the alias must not be empty"})
	}
	if !strings.Contains(destination, "@") {
		return kit.Failure[EmailForward](InvalidForwardError{Reason: "the destination must be an email address"})
	}
	return component.gateway.CreateEmailForward(ctx, session, domainName, strings.TrimSpace(alias), strings.TrimSpace(destination))
}

func (component *Component) DeleteEmailForward(ctx context.Context, session identity.Session, domainName string, forwardID int64) kit.Outcome[kit.Nothing] {
	return component.gateway.DeleteEmailForward(ctx, session, domainName, forwardID)
}

// DSRecord is a delegation signer record pushed to the parent zone; the
// fields mirror what registries expect verbatim.
type DSRecord struct {
	ID         int64
	KeyTag     string
	Algorithm  string
	DigestType string
	Digest     string
}

func ValidateDSRecord(record DSRecord) error {
	for field, value := range map[string]string{
		"key tag": record.KeyTag, "algorithm": record.Algorithm, "digest type": record.DigestType,
	} {
		if strings.TrimSpace(value) == "" {
			return InvalidForwardError{Reason: "the " + field + " must not be empty"}
		}
	}
	digest := strings.TrimSpace(record.Digest)
	if len(digest) < 32 {
		return InvalidForwardError{Reason: "the digest looks too short to be a DS digest"}
	}
	return nil
}

func (component *Component) DSRecords(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]DSRecord] {
	return component.gateway.DSRecords(ctx, session, domainName)
}

func (component *Component) CreateDSRecord(ctx context.Context, session identity.Session, domainName string, record DSRecord) kit.Outcome[DSRecord] {
	if err := ValidateDSRecord(record); err != nil {
		return kit.Failure[DSRecord](err)
	}
	return component.gateway.CreateDSRecord(ctx, session, domainName, record)
}

func (component *Component) DeleteDSRecord(ctx context.Context, session identity.Session, domainName string, dsRecordID int64) kit.Outcome[kit.Nothing] {
	return component.gateway.DeleteDSRecord(ctx, session, domainName, dsRecordID)
}

// AddDomain hosts an existing domain's DNS on the account without touching
// its registration; DNSimple creates the zone alongside.
func (component *Component) AddDomain(ctx context.Context, session identity.Session, domainName string) kit.Outcome[Domain] {
	domainName = strings.ToLower(strings.TrimSpace(domainName))
	if err := ValidateDomainName(domainName); err != nil {
		return kit.Failure[Domain](err)
	}
	added := component.gateway.AddDomain(ctx, session, domainName)
	if !added.Failed() {
		slog.InfoContext(ctx, "domain added", "domain", domainName)
	}
	return added
}

// DeleteDomain removes the domain and its hosted DNS from the account; a
// registered domain stays registered, it just stops resolving from here.
func (component *Component) DeleteDomain(ctx context.Context, session identity.Session, domainName string) kit.Outcome[kit.Nothing] {
	deleted := component.gateway.DeleteDomain(ctx, session, domainName)
	if !deleted.Failed() {
		slog.InfoContext(ctx, "domain deleted", "domain", domainName)
	}
	return deleted
}
