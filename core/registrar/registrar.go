// Package registrar is the Component for registrar operations (DESIGN.md
// §3.1): availability checks and the RegisterDomain Business Transaction,
// the first async workflow in the system.
package registrar

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type Availability struct {
	Domain    string
	Available bool
	Premium   bool
}

type Contact struct {
	ID    int64
	Label string
}

type Registration struct {
	ID    int64
	State string
}

// Registration states DNSimple reports; everything else counts as still
// in flight and keeps the poll alive.
const (
	StateRegistered = "registered"
	StateFailed     = "failed"
	StateCancelled  = "cancelled"
)

func (registration Registration) Terminal() bool {
	return registration.State == StateRegistered ||
		registration.State == StateFailed ||
		registration.State == StateCancelled
}

var (
	ErrDomainTaken = errors.New("registrar: the domain is not available")
	ErrNoContacts  = errors.New("registrar: the account has no contacts; create one in the Contacts screen first")
)

type InvalidDomainError struct {
	Reason string
}

func (invalid InvalidDomainError) Error() string {
	return "registrar: invalid domain name: " + invalid.Reason
}

var domainNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

func ValidateDomainName(name string) error {
	if !domainNamePattern.MatchString(name) {
		return InvalidDomainError{Reason: fmt.Sprintf("%q is not a registrable name (lowercase, digits, hyphens, at least one dot)", name)}
	}
	return nil
}

// Quote is what a registration will cost, from the registry's own mouth.
type Quote struct {
	Available         bool
	Premium           bool
	RegistrationPrice float64
}

// Renewal is DNSimple's acceptance of a renew order.
type Renewal struct {
	ID    int64
	State string
}

// Transfer is an inbound domain transfer advancing through registry states.
type Transfer struct {
	ID    int64
	State string
}

const (
	// DNSimple's terminal success state is "transferred", not "completed"
	// (spec enum: cancelled, new, transferring, transferred, failed).
	TransferCompleted = "transferred"
	TransferFailed    = "failed"
	TransferCancelled = "cancelled"
)

func (transfer Transfer) Terminal() bool {
	return transfer.State == TransferCompleted ||
		transfer.State == TransferFailed ||
		transfer.State == TransferCancelled
}

// Restore is DNSimple's acceptance of a restore order for an expired domain.
type Restore struct {
	ID    int64
	State string
}

// RegistrantChange moves a domain to a different registrant contact; some
// registries take days and demand email approval (IRT lock).
type RegistrantChange struct {
	ID        int64
	State     string
	DomainID  int64
	ContactID int64
}

const (
	RegistrantChangeCompleted = "completed"
	RegistrantChangeCancelled = "cancelled"
)

func (change RegistrantChange) Terminal() bool {
	return change.State == RegistrantChangeCompleted || change.State == RegistrantChangeCancelled
}

// RegistrantChangeCheck is what DNSimple requires before the change.
type RegistrantChangeCheck struct {
	RequiresExtendedAttributes bool
	RegistryOwnerChange        bool
}

// ErrNeedsExtendedAttributes marks TLDs whose registrant changes require
// extra registry data the TUI does not collect yet.
var ErrNeedsExtendedAttributes = errors.New(
	"registrar: this TLD requires extended attributes for a registrant change; use dnsimple.com for now")

var hostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+\.?$`)

// ValidateDelegation enforces what registries enforce: at least 2 name
// servers, at most 6, all well-formed hostnames.
func ValidateDelegation(nameServers []string) error {
	if len(nameServers) < 2 || len(nameServers) > 6 {
		return InvalidDomainError{Reason: "delegation needs between 2 and 6 name servers"}
	}
	for _, nameServer := range nameServers {
		if !hostnamePattern.MatchString(nameServer) {
			return InvalidDomainError{Reason: fmt.Sprintf("%q is not a valid name server hostname", nameServer)}
		}
	}
	return nil
}

type Gateway interface {
	CheckAvailability(ctx context.Context, session identity.Session, domainName string) kit.Outcome[Availability]
	Prices(ctx context.Context, session identity.Session, domainName string) kit.Outcome[float64]
	Contacts(ctx context.Context, session identity.Session) kit.Outcome[[]Contact]
	Register(ctx context.Context, session identity.Session, domainName string, contactID int64) kit.Outcome[Registration]
	RegistrationState(ctx context.Context, session identity.Session, domainName string, registrationID int64) kit.Outcome[Registration]
	SetAutoRenewal(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing]
	WhoisPrivacy(ctx context.Context, session identity.Session, domainName string) kit.Outcome[bool]
	SetWhoisPrivacy(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing]
	TransferLock(ctx context.Context, session identity.Session, domainName string) kit.Outcome[bool]
	SetTransferLock(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing]
	Delegation(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]string]
	SetDelegation(ctx context.Context, session identity.Session, domainName string, nameServers []string) kit.Outcome[kit.Nothing]
	Renew(ctx context.Context, session identity.Session, domainName string) kit.Outcome[Renewal]
	Transfer(ctx context.Context, session identity.Session, domainName string, contactID int64, authCode string) kit.Outcome[Transfer]
	TransferState(ctx context.Context, session identity.Session, domainName string, transferID int64) kit.Outcome[Transfer]
	AuthorizeTransferOut(ctx context.Context, session identity.Session, domainName string) kit.Outcome[kit.Nothing]
	Restore(ctx context.Context, session identity.Session, domainName string) kit.Outcome[Restore]
	CheckRegistrantChange(ctx context.Context, session identity.Session, domainID, contactID int64) kit.Outcome[RegistrantChangeCheck]
	CreateRegistrantChange(ctx context.Context, session identity.Session, domainID, contactID int64) kit.Outcome[RegistrantChange]
	RegistrantChangeState(ctx context.Context, session identity.Session, changeID int64) kit.Outcome[RegistrantChange]
	SetVanityNameServers(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing]
	RegistrantChanges(ctx context.Context, session identity.Session) kit.Outcome[[]RegistrantChange]
	CancelRegistrantChange(ctx context.Context, session identity.Session, changeID int64) kit.Outcome[kit.Nothing]
	CancelTransfer(ctx context.Context, session identity.Session, domainName string, transferID int64) kit.Outcome[kit.Nothing]
}

// PollQueue is the port to the job runner: RegisterDomain and TransferDomain
// hand the polling over and return; the queue owns retries and survives
// restarts.
type PollQueue interface {
	EnqueueRegistrationPoll(ctx context.Context, tenantID int64, domainName string, registrationID int64) kit.Outcome[kit.Nothing]
	EnqueueTransferPoll(ctx context.Context, tenantID int64, domainName string, transferID int64) kit.Outcome[kit.Nothing]
	EnqueueRegistrantChangePoll(ctx context.Context, tenantID, changeID int64) kit.Outcome[kit.Nothing]
}
