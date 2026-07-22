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

func TestAwaitLinkWakesAcrossConnectionsWhenTheHandshakeLands(test *testing.T) {
	// Given an SSH session parked on its link code
	repository := NewIdentityRepository(testPool(test))
	link := identity.NewPendingLink("SHA256:laptop")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
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
