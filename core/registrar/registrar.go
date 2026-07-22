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
	ErrNoContacts  = errors.New("registrar: the account has no contacts; create one at dnsimple.com first")
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

type Gateway interface {
	CheckAvailability(ctx context.Context, session identity.Session, domainName string) kit.Outcome[Availability]
	Contacts(ctx context.Context, session identity.Session) kit.Outcome[[]Contact]
	Register(ctx context.Context, session identity.Session, domainName string, contactID int64) kit.Outcome[Registration]
	RegistrationState(ctx context.Context, session identity.Session, domainName string, registrationID int64) kit.Outcome[Registration]
	SetAutoRenewal(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing]
}

// PollQueue is the port to the job runner: RegisterDomain hands the polling
// over and returns; the queue owns retries and survives restarts.
type PollQueue interface {
	EnqueueRegistrationPoll(ctx context.Context, tenantID int64, domainName string, registrationID int64) kit.Outcome[kit.Nothing]
}
