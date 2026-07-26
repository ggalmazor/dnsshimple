// Package memory implements the persistence ports in process memory: the
// grey-box test double for everything above the storage port, and a
// database-free mode for local hacking. Not for production; state dies with
// the process and never crosses instances.
package memory

import (
	"context"
	"sync"
	"time"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type IdentityRepository struct {
	guard               sync.Mutex
	pendingByCode       map[string]identity.PendingLink
	recordByFingerprint map[string]identity.TenantRecord
	waiters             map[string][]chan identity.TenantRecord // by fingerprint
	nextTenantID        int64
}

func NewIdentityRepository() *IdentityRepository {
	return &IdentityRepository{
		pendingByCode:       map[string]identity.PendingLink{},
		recordByFingerprint: map[string]identity.TenantRecord{},
		waiters:             map[string][]chan identity.TenantRecord{},
	}
}

var _ identity.Repository = (*IdentityRepository)(nil)

func (repository *IdentityRepository) TenantBySSHKey(_ context.Context, fingerprint string) kit.Outcome[identity.TenantRecord] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	record, known := repository.recordByFingerprint[fingerprint]
	if !known {
		return kit.Failure[identity.TenantRecord](identity.ErrUnknownFingerprint)
	}
	return kit.Success(record)
}

func (repository *IdentityRepository) TenantByID(_ context.Context, tenantID int64) kit.Outcome[identity.TenantRecord] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	for _, record := range repository.recordByFingerprint {
		if record.ID == tenantID {
			return kit.Success(record)
		}
	}
	return kit.Failure[identity.TenantRecord](identity.ErrUnknownTenant)
}

func (repository *IdentityRepository) SavePendingLink(_ context.Context, link identity.PendingLink) kit.Outcome[kit.Nothing] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	repository.pendingByCode[link.Code] = link
	return kit.Done()
}

func (repository *IdentityRepository) PeekPendingLink(_ context.Context, code string, notBefore time.Time) kit.Outcome[identity.PendingLink] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	link, found := repository.pendingByCode[code]
	if !found || link.StartedAt.Before(notBefore) {
		return kit.Failure[identity.PendingLink](identity.ErrLinkCodeUnknown)
	}
	return kit.Success(link)
}

func (repository *IdentityRepository) ConsumePendingLink(_ context.Context, code string, notBefore time.Time) kit.Outcome[identity.PendingLink] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	link, found := repository.pendingByCode[code]
	if !found || link.StartedAt.Before(notBefore) {
		return kit.Failure[identity.PendingLink](identity.ErrLinkCodeUnknown)
	}
	delete(repository.pendingByCode, code)
	return kit.Success(link)
}

func (repository *IdentityRepository) BindTenant(_ context.Context, fingerprint string, remote identity.RemoteIdentity, sealedToken []byte) kit.Outcome[identity.Tenant] {
	repository.guard.Lock()
	defer repository.guard.Unlock()

	// Upsert by DNSimple account, mirroring the Postgres semantics.
	var tenant identity.Tenant
	for _, existing := range repository.recordByFingerprint {
		if existing.AccountID == remote.AccountID {
			tenant = existing.Tenant
			break
		}
	}
	if tenant.ID == 0 {
		repository.nextTenantID++
		tenant = identity.Tenant{ID: repository.nextTenantID, AccountID: remote.AccountID, Email: remote.Email}
	}
	tenant.Email = remote.Email
	repository.recordByFingerprint[fingerprint] = identity.TenantRecord{Tenant: tenant, SealedToken: sealedToken}
	return kit.Success(tenant)
}

func (repository *IdentityRepository) AnnounceLink(_ context.Context, code, fingerprint string) kit.Outcome[kit.Nothing] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	record, bound := repository.recordByFingerprint[fingerprint]
	if !bound {
		return kit.Done() // nothing to wake with; AwaitLink's recheck covers it
	}
	for _, waiter := range repository.waiters[fingerprint] {
		waiter <- record // buffered(1)
	}
	delete(repository.waiters, fingerprint)
	return kit.Done()
}

func (repository *IdentityRepository) AwaitLink(ctx context.Context, code, fingerprint string) kit.Outcome[identity.TenantRecord] {
	repository.guard.Lock()
	// Watch the code, not the binding: a relinking fingerprint is already
	// bound, and that must not answer a wait for a fresh code.
	if _, stillPending := repository.pendingByCode[code]; !stillPending {
		if record, bound := repository.recordByFingerprint[fingerprint]; bound {
			repository.guard.Unlock()
			return kit.Success(record)
		}
	}
	waiter := make(chan identity.TenantRecord, 1)
	repository.waiters[fingerprint] = append(repository.waiters[fingerprint], waiter)
	repository.guard.Unlock()

	select {
	case record := <-waiter:
		return kit.Success(record)
	case <-ctx.Done():
		return kit.Failure[identity.TenantRecord](ctx.Err())
	}
}

func (repository *IdentityRepository) TenantsBySSHKey(_ context.Context, fingerprint string) kit.Outcome[[]identity.TenantRecord] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	record, known := repository.recordByFingerprint[fingerprint]
	if !known {
		return kit.Success([]identity.TenantRecord{})
	}
	// The memory adapter backs the spike and single-account tests only.
	return kit.Success([]identity.TenantRecord{record})
}

func (repository *IdentityRepository) Bindings(context.Context) kit.Outcome[[]identity.KeyBinding] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	var bindings []identity.KeyBinding
	for fingerprint, record := range repository.recordByFingerprint {
		bindings = append(bindings, identity.KeyBinding{Fingerprint: fingerprint, Tenant: record.Tenant})
	}
	return kit.Success(bindings)
}

func (repository *IdentityRepository) Census(context.Context) kit.Outcome[identity.Census] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	tenants := map[int64]bool{}
	for _, record := range repository.recordByFingerprint {
		tenants[record.ID] = true
	}
	return kit.Success(identity.Census{
		Tenants:      int64(len(tenants)),
		Bindings:     int64(len(repository.recordByFingerprint)),
		PendingLinks: int64(len(repository.pendingByCode)),
	})
}

func (repository *IdentityRepository) DeletePendingLinksBefore(_ context.Context, cutoff time.Time) kit.Outcome[int64] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	var pruned int64
	for code, link := range repository.pendingByCode {
		if link.StartedAt.Before(cutoff) {
			delete(repository.pendingByCode, code)
			pruned++
		}
	}
	return kit.Success(pruned)
}

func (repository *IdentityRepository) TouchSSHKey(context.Context, string, int64) kit.Outcome[kit.Nothing] {
	return kit.Done()
}

func (repository *IdentityRepository) UnlinkSSHKey(_ context.Context, fingerprint string, _ int64) kit.Outcome[kit.Nothing] {
	repository.guard.Lock()
	defer repository.guard.Unlock()
	delete(repository.recordByFingerprint, fingerprint)
	return kit.Done()
}
