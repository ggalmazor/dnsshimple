package identity

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"dnsshimple/core/telemetry"
	"dnsshimple/kit"
)

// The Component tests are grey-box at the Component port: in-memory port
// fakes at the far edges, assertions on observable outcomes only. The real
// Postgres repository has its own grey-box suite in adapters/postgres.

func TestCompleteLinkRunsTheWholeRailwayAndAnnouncesTheTenant(test *testing.T) {
	// Given a pending link, a DNSimple that recognizes the grant, and a vault
	harness := newHarness()
	pending := harness.component.BeginLink(context.Background(), "SHA256:laptop")
	if pending.Failed() {
		test.Fatalf("BeginLink failed: %v", pending.Error())
	}

	// When the bridge completes the handshake with a sloppily typed code
	typed := strings.ToLower(DisplayLinkCode(pending.Value().Code))
	completed := harness.component.CompleteLink(context.Background(), typed, "grant-42")

	// Then the tenant is bound to the fingerprint with a sealed token
	if completed.Failed() {
		test.Fatalf("CompleteLink failed: %v", completed.Error())
	}
	if completed.Value().AccountID != 2416 || completed.Value().Email != "guille@sendia.org" {
		test.Fatalf("wrong tenant bound: %+v", completed.Value())
	}
	session := harness.component.SessionFor(context.Background(), "SHA256:laptop")
	if session.Failed() || session.Value().AccessToken != "token-from-dnsimple" {
		test.Fatalf("the fingerprint did not resolve to a working session: %+v / %v",
			session.Value(), session.Error())
	}

	// And the announcement carries the code and the fingerprint
	if harness.repository.announcedCode != pending.Value().Code ||
		harness.repository.announcedFingerprint != "SHA256:laptop" {
		test.Fatalf("announcement missing or wrong: %+v", harness.repository)
	}

	// And the code is single-use
	retry := harness.component.CompleteLink(context.Background(), typed, "grant-42")
	if !retry.Failed() || !errors.Is(retry.Error(), ErrLinkCodeUnknown) {
		test.Fatalf("a second CompleteLink must fail with ErrLinkCodeUnknown, got %v", retry.Error())
	}
}

func TestCompleteLinkNeverCallsDNSimpleForAnUnknownCode(test *testing.T) {
	// Given no pending link
	harness := newHarness()

	// When the bridge tries to complete a made-up code
	outcome := harness.component.CompleteLink(context.Background(), "AAAA-2222", "grant-42")

	// Then the railway short-circuits before DNSimple is ever called
	if !outcome.Failed() || !errors.Is(outcome.Error(), ErrLinkCodeUnknown) {
		test.Fatalf("expected ErrLinkCodeUnknown, got %v", outcome.Error())
	}
	if harness.dnsimple.redeemCalls != 0 {
		test.Fatalf("DNSimple was called %d times for an unknown code", harness.dnsimple.redeemCalls)
	}
}

func TestSessionForFailsClosedWhenTheFingerprintIsUnknown(test *testing.T) {
	harness := newHarness()

	outcome := harness.component.SessionFor(context.Background(), "SHA256:stranger")

	if !outcome.Failed() || !errors.Is(outcome.Error(), ErrUnknownFingerprint) {
		test.Fatalf("expected ErrUnknownFingerprint, got %v", outcome.Error())
	}
}

// --- harness and port fakes -----------------------------------------------

type harness struct {
	component  *Component
	repository *fakeRepository
	dnsimple   *fakeAuthenticator
}

func newHarness() *harness {
	repository := &fakeRepository{
		pendingByCode:       map[string]PendingLink{},
		recordByFingerprint: map[string]TenantRecord{},
	}
	dnsimple := &fakeAuthenticator{}
	return &harness{
		component:  NewComponent(repository, reversibleVault{}, dnsimple, telemetry.NewMetrics()),
		repository: repository,
		dnsimple:   dnsimple,
	}
}

type fakeRepository struct {
	guard                sync.Mutex
	pendingByCode        map[string]PendingLink
	recordByFingerprint  map[string]TenantRecord
	announcedCode        string
	announcedFingerprint string
}

func (repository *fakeRepository) TenantBySSHKey(_ context.Context, fingerprint string) kit.Outcome[TenantRecord] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	record, known := repository.recordByFingerprint[fingerprint]
	if !known {
		return kit.Failure[TenantRecord](ErrUnknownFingerprint)
	}
	return kit.Success(record)
}

func (repository *fakeRepository) SavePendingLink(_ context.Context, link PendingLink) kit.Outcome[kit.Nothing] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	repository.pendingByCode[link.Code] = link
	return kit.Done()
}

func (repository *fakeRepository) PeekPendingLink(_ context.Context, code string, notBefore time.Time) kit.Outcome[PendingLink] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	link, found := repository.pendingByCode[code]
	if !found || link.StartedAt.Before(notBefore) {
		return kit.Failure[PendingLink](ErrLinkCodeUnknown)
	}
	return kit.Success(link)
}

func (repository *fakeRepository) ConsumePendingLink(_ context.Context, code string, notBefore time.Time) kit.Outcome[PendingLink] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	link, found := repository.pendingByCode[code]
	if !found || link.StartedAt.Before(notBefore) {
		return kit.Failure[PendingLink](ErrLinkCodeUnknown)
	}
	delete(repository.pendingByCode, code)
	return kit.Success(link)
}

func (repository *fakeRepository) BindTenant(_ context.Context, fingerprint string, remote RemoteIdentity, sealedToken []byte) kit.Outcome[Tenant] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	tenant := Tenant{ID: 1, AccountID: remote.AccountID, Email: remote.Email}
	repository.recordByFingerprint[fingerprint] = TenantRecord{Tenant: tenant, SealedToken: sealedToken}
	return kit.Success(tenant)
}

func (repository *fakeRepository) AnnounceLink(_ context.Context, code, fingerprint string) kit.Outcome[kit.Nothing] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	repository.announcedCode = code
	repository.announcedFingerprint = fingerprint
	return kit.Done()
}

func (repository *fakeRepository) AwaitLink(ctx context.Context, code string) kit.Outcome[TenantRecord] {
	// The Component tests never park; the Postgres suite covers waiting.
	return kit.Failure[TenantRecord](context.Canceled)
}

// reversibleVault "seals" by prefixing, so tests can assert without crypto.
type reversibleVault struct{}

func (reversibleVault) Seal(accessToken string) kit.Outcome[[]byte] {
	return kit.Success([]byte("sealed:" + accessToken))
}

func (reversibleVault) Open(sealedToken []byte) kit.Outcome[string] {
	return kit.Success(strings.TrimPrefix(string(sealedToken), "sealed:"))
}

type fakeAuthenticator struct {
	redeemCalls int
}

func (authenticator *fakeAuthenticator) AuthorizationPageURL(linkCode string) string {
	return "https://sandbox.dnsimple.com/oauth/authorize?state=" + linkCode
}

func (authenticator *fakeAuthenticator) Redeem(_ context.Context, grantCode, linkCode string) kit.Outcome[RemoteIdentity] {
	authenticator.redeemCalls++
	if grantCode != "grant-42" {
		return kit.Failure[RemoteIdentity](errors.New("unknown grant"))
	}
	return kit.Success(RemoteIdentity{
		AccountID:   2416,
		Email:       "guille@sendia.org",
		AccessToken: "token-from-dnsimple",
	})
}
