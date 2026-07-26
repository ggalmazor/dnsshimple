package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"dnsshimple/core/identity"
)

func TestPendingLinksAreSingleUseAndExpire(test *testing.T) {
	// Given a saved pending link
	repository := NewIdentityRepository(testPool(test))
	ctx := context.Background()
	link := identity.NewPendingLink("SHA256:laptop")
	if saved := repository.SavePendingLink(ctx, link); saved.Failed() {
		test.Fatalf("saving: %v", saved.Error())
	}
	notBefore := time.Now().Add(-identity.LinkCodeLifetime)

	// When it is peeked, consumed, and consumed again
	if peeked := repository.PeekPendingLink(ctx, link.Code, notBefore); peeked.Failed() {
		test.Fatalf("peek must succeed on a live code: %v", peeked.Error())
	}
	first := repository.ConsumePendingLink(ctx, link.Code, notBefore)
	second := repository.ConsumePendingLink(ctx, link.Code, notBefore)

	// Then exactly the first consume wins
	if first.Failed() || first.Value().Fingerprint != "SHA256:laptop" {
		test.Fatalf("first consume: %+v / %v", first.Value(), first.Error())
	}
	if !second.Failed() || !errors.Is(second.Error(), identity.ErrLinkCodeUnknown) {
		test.Fatalf("second consume must fail with ErrLinkCodeUnknown, got %v", second.Error())
	}

	// And an expired link is invisible to both peek and consume
	expired := identity.PendingLink{Code: "EXPIRED2", Fingerprint: "SHA256:old", StartedAt: time.Now().Add(-time.Hour)}
	_ = repository.SavePendingLink(ctx, expired)
	if outcome := repository.ConsumePendingLink(ctx, expired.Code, notBefore); !outcome.Failed() {
		test.Fatal("an expired code must not be consumable")
	}
}

func TestBindTenantUpsertsByAccountAndRebindsKeys(test *testing.T) {
	// Given one DNSimple account linking from two machines
	repository := NewIdentityRepository(testPool(test))
	ctx := context.Background()
	remote := identity.RemoteIdentity{AccountID: 2416, Email: "guille@sendia.org"}

	// When both fingerprints bind, the second with a rotated sealed token
	first := repository.BindTenant(ctx, "SHA256:laptop", remote, []byte("sealed-one"))
	second := repository.BindTenant(ctx, "SHA256:desktop", remote, []byte("sealed-two"))

	// Then both resolve to the same tenant carrying the latest token
	if first.Failed() || second.Failed() {
		test.Fatalf("binding failed: %v / %v", first.Error(), second.Error())
	}
	if first.Value().ID != second.Value().ID {
		test.Fatalf("one account became two tenants: %d and %d", first.Value().ID, second.Value().ID)
	}
	for _, fingerprint := range []string{"SHA256:laptop", "SHA256:desktop"} {
		record := repository.TenantBySSHKey(ctx, fingerprint)
		if record.Failed() || string(record.Value().SealedToken) != "sealed-two" {
			test.Fatalf("%s: %+v / %v", fingerprint, record.Value(), record.Error())
		}
	}

	// And an unknown fingerprint fails closed
	if outcome := repository.TenantBySSHKey(ctx, "SHA256:stranger"); !errors.Is(outcome.Error(), identity.ErrUnknownFingerprint) {
		test.Fatalf("expected ErrUnknownFingerprint, got %v", outcome.Error())
	}
}

func TestBindingsListEveryKeyAccountAttachmentMostRecentlyUsedFirst(test *testing.T) {
	// Given two keys on one account and a second account on the first key
	repository := NewIdentityRepository(testPool(test))
	ctx := context.Background()
	first := identity.RemoteIdentity{AccountID: 2416, Email: "guille@sendia.org"}
	second := identity.RemoteIdentity{AccountID: 9001, Email: "ops@sendia.org"}
	_ = repository.BindTenant(ctx, "SHA256:laptop", first, []byte("sealed"))
	_ = repository.BindTenant(ctx, "SHA256:desktop", first, []byte("sealed"))
	bound := repository.BindTenant(ctx, "SHA256:laptop", second, []byte("sealed"))

	// When the laptop's first binding is used again and bindings are listed
	if touched := repository.TouchSSHKey(ctx, "SHA256:laptop", 1); touched.Failed() {
		test.Fatalf("touching: %v", touched.Error())
	}
	listed := repository.Bindings(ctx)

	// Then all three attachments show, the touched one first, tenants attached
	if listed.Failed() {
		test.Fatalf("listing bindings: %v", listed.Error())
	}
	bindings := listed.Value()
	if len(bindings) != 3 {
		test.Fatalf("expected 3 bindings, got %d", len(bindings))
	}
	lead := bindings[0]
	if lead.Fingerprint != "SHA256:laptop" || lead.Tenant.AccountID != 2416 {
		test.Fatalf("the touched binding must lead, got %+v", lead)
	}
	if lead.Tenant.Email != "guille@sendia.org" || lead.LastUsedAt.IsZero() {
		test.Fatalf("the binding must carry its tenant and recency, got %+v", lead)
	}
	if bound.Failed() {
		test.Fatalf("binding the second account: %v", bound.Error())
	}
}

func TestDeletingStalePendingLinksSparesLiveOnes(test *testing.T) {
	// Given one live and one stale pending link
	repository := NewIdentityRepository(testPool(test))
	ctx := context.Background()
	live := identity.NewPendingLink("SHA256:laptop")
	_ = repository.SavePendingLink(ctx, live)
	stale := identity.PendingLink{Code: "DUSTY123", Fingerprint: "SHA256:old", StartedAt: time.Now().Add(-time.Hour)}
	_ = repository.SavePendingLink(ctx, stale)

	// When links started before the lifetime cutoff are deleted
	pruned := repository.DeletePendingLinksBefore(ctx, time.Now().Add(-identity.LinkCodeLifetime))

	// Then exactly the stale one goes and the live one still consumes
	if pruned.Failed() || pruned.Value() != 1 {
		test.Fatalf("expected 1 pruned link, got %d / %v", pruned.Value(), pruned.Error())
	}
	notBefore := time.Now().Add(-identity.LinkCodeLifetime)
	if outcome := repository.ConsumePendingLink(ctx, live.Code, notBefore); outcome.Failed() {
		test.Fatalf("the live link must survive the prune: %v", outcome.Error())
	}
}

func TestAwaitLinkWakesAcrossConnectionsWhenTheHandshakeLands(test *testing.T) {
	// Given an SSH session parked on its link code
	repository := NewIdentityRepository(testPool(test))
	link := identity.NewPendingLink("SHA256:laptop")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// The real flow persists the code before parking; the wait's recheck
	// reads it to tell "still pending" from "already handled".
	if saved := repository.SavePendingLink(ctx, link); saved.Failed() {
		test.Fatalf("saving the pending link: %v", saved.Error())
	}
	woken := make(chan string, 1)
	go func() {
		outcome := repository.AwaitLink(ctx, link.Code, link.Fingerprint)
		if outcome.Failed() {
			woken <- "failed: " + outcome.Error().Error()
			return
		}
		woken <- outcome.Value().Email
	}()

	// When the handshake lands on another connection (as the bridge would)
	time.Sleep(200 * time.Millisecond) // let the waiter reach LISTEN
	remote := identity.RemoteIdentity{AccountID: 2416, Email: "guille@sendia.org"}
	if bound := repository.BindTenant(ctx, link.Fingerprint, remote, []byte("sealed")); bound.Failed() {
		test.Fatalf("binding: %v", bound.Error())
	}
	if announced := repository.AnnounceLink(ctx, link.Code, link.Fingerprint); announced.Failed() {
		test.Fatalf("announcing: %v", announced.Error())
	}

	// Then the parked session wakes with the tenant
	select {
	case result := <-woken:
		if result != "guille@sendia.org" {
			test.Fatalf("waiter woke wrong: %s", result)
		}
	case <-ctx.Done():
		test.Fatal("the parked session never woke")
	}
}

func TestAwaitLinkReturnsImmediatelyWhenTheHandshakeAlreadyLanded(test *testing.T) {
	// Given a handshake that landed before the session started waiting
	repository := NewIdentityRepository(testPool(test))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	remote := identity.RemoteIdentity{AccountID: 2416, Email: "guille@sendia.org"}
	if bound := repository.BindTenant(ctx, "SHA256:laptop", remote, []byte("sealed")); bound.Failed() {
		test.Fatalf("binding: %v", bound.Error())
	}

	// When the session begins to wait (no announcement will ever come)
	outcome := repository.AwaitLink(ctx, "NEVER222", "SHA256:laptop")

	// Then it does not park: the bind is already visible
	if outcome.Failed() || outcome.Value().Email != "guille@sendia.org" {
		test.Fatalf("expected an immediate wake, got %+v / %v", outcome.Value(), outcome.Error())
	}
}

func TestAKeyAccumulatesAccountsAndSwitchingReordersThem(test *testing.T) {
	repository := NewIdentityRepository(testPool(test))
	ctx := context.Background()
	fingerprint := "SHA256:multiaccount"

	first := repository.BindTenant(ctx, fingerprint,
		identity.RemoteIdentity{AccountID: 100, Email: "one@sendia.org"}, []byte("sealed-1"))
	if first.Failed() {
		test.Fatalf("bind first: %v", first.Error())
	}
	second := repository.BindTenant(ctx, fingerprint,
		identity.RemoteIdentity{AccountID: 200, Email: "two@sendia.org"}, []byte("sealed-2"))
	if second.Failed() {
		test.Fatalf("bind second: %v", second.Error())
	}

	listed := repository.TenantsBySSHKey(ctx, fingerprint)
	if listed.Failed() || len(listed.Value()) != 2 {
		test.Fatalf("both accounts must be linked: %+v / %v", listed.Value(), listed.Error())
	}
	// The connection greets with the account whose handshake landed last.
	current := repository.TenantBySSHKey(ctx, fingerprint)
	if current.Failed() || current.Value().AccountID != 200 {
		test.Fatalf("most recent must lead: %+v / %v", current.Value(), current.Error())
	}

	if touched := repository.TouchSSHKey(ctx, fingerprint, first.Value().ID); touched.Failed() {
		test.Fatalf("touch: %v", touched.Error())
	}
	switched := repository.TenantBySSHKey(ctx, fingerprint)
	if switched.Failed() || switched.Value().AccountID != 100 {
		test.Fatalf("switching must reorder: %+v / %v", switched.Value(), switched.Error())
	}
}

// A relinking key is already bound; the wait for a fresh code must park on
// the announcement instead of answering with the old binding (the bug that
// made "link another" a no-op).
func TestAwaitLinkForAFreshCodeIgnoresTheExistingBinding(test *testing.T) {
	repository := NewIdentityRepository(testPool(test))
	ctx := context.Background()
	fingerprint := "SHA256:relinker"

	bound := repository.BindTenant(ctx, fingerprint,
		identity.RemoteIdentity{AccountID: 300, Email: "current@sendia.org"}, []byte("sealed"))
	if bound.Failed() {
		test.Fatalf("bind: %v", bound.Error())
	}
	link := identity.NewPendingLink(fingerprint)
	if saved := repository.SavePendingLink(ctx, link); saved.Failed() {
		test.Fatalf("save pending: %v", saved.Error())
	}

	waitCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	awaited := repository.AwaitLink(waitCtx, link.Code, fingerprint)
	if !awaited.Failed() {
		test.Fatalf("the wait must not resolve from the pre-existing binding: %+v", awaited.Value())
	}

	// Once the new handshake lands, the same wait resolves with the fresh
	// account.
	relinked := identity.NewPendingLink(fingerprint)
	if saved := repository.SavePendingLink(ctx, relinked); saved.Failed() {
		test.Fatalf("save second pending: %v", saved.Error())
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		if consumed := repository.ConsumePendingLink(context.Background(), relinked.Code, time.Now().Add(-time.Hour)); consumed.Failed() {
			return
		}
		repository.BindTenant(context.Background(), fingerprint,
			identity.RemoteIdentity{AccountID: 400, Email: "fresh@sendia.org"}, []byte("sealed-2"))
		repository.AnnounceLink(context.Background(), relinked.Code, fingerprint)
	}()
	resolveCtx, cancelResolve := context.WithTimeout(ctx, 5*time.Second)
	defer cancelResolve()
	resolved := repository.AwaitLink(resolveCtx, relinked.Code, fingerprint)
	if resolved.Failed() || resolved.Value().AccountID != 400 {
		test.Fatalf("the announced handshake must resolve with the new account: %+v / %v", resolved.Value(), resolved.Error())
	}
}

func TestUnlinkingRemovesTheBindingAndPrunesOrphanedTenants(test *testing.T) {
	repository := NewIdentityRepository(testPool(test))
	ctx := context.Background()

	shared := repository.BindTenant(ctx, "SHA256:keyA",
		identity.RemoteIdentity{AccountID: 500, Email: "shared@sendia.org"}, []byte("sealed"))
	if shared.Failed() {
		test.Fatalf("bind: %v", shared.Error())
	}
	if second := repository.BindTenant(ctx, "SHA256:keyB",
		identity.RemoteIdentity{AccountID: 500, Email: "shared@sendia.org"}, []byte("sealed")); second.Failed() {
		test.Fatalf("bind second key: %v", second.Error())
	}

	// Unlinking one key keeps the tenant for the other.
	if unlinked := repository.UnlinkSSHKey(ctx, "SHA256:keyA", shared.Value().ID); unlinked.Failed() {
		test.Fatalf("unlink: %v", unlinked.Error())
	}
	if gone := repository.TenantBySSHKey(ctx, "SHA256:keyA"); !gone.Failed() {
		test.Fatal("keyA must be unlinked")
	}
	if kept := repository.TenantBySSHKey(ctx, "SHA256:keyB"); kept.Failed() {
		test.Fatalf("keyB must keep the tenant: %v", kept.Error())
	}

	// Unlinking the last key prunes the tenant and its sealed token.
	if unlinked := repository.UnlinkSSHKey(ctx, "SHA256:keyB", shared.Value().ID); unlinked.Failed() {
		test.Fatalf("unlink last: %v", unlinked.Error())
	}
	if pruned := repository.TenantByID(ctx, shared.Value().ID); !pruned.Failed() {
		test.Fatal("the orphaned tenant must be pruned")
	}
}
