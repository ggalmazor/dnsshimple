package dnsimple

import (
	"context"
	"errors"
	"testing"

	"dnsshimple/core/identity"
	"dnsshimple/core/pushes"
	"dnsshimple/core/services"
	"dnsshimple/core/telemetry"
)

func TestServicesApplyAndUnapplyRoundTrip(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	component := services.NewComponent(
		NewServicesGateway(NewClient(server.URL, server.Client(), telemetry.NewMetrics())), telemetry.NewMetrics())
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()
	fake.AddDomain("ggalmazor.com", "registered")

	available := component.AvailableServices(ctx, session)
	if available.Failed() || len(available.Value()) != 2 {
		test.Fatalf("available: %+v / %v", available.Value(), available.Error())
	}
	if applied := component.ApplyService(ctx, session, "ggalmazor.com", available.Value()[0].ID); applied.Failed() {
		test.Fatalf("apply: %v", applied.Error())
	}
	appliedNow := component.AppliedServices(ctx, session, "ggalmazor.com")
	if appliedNow.Failed() || len(appliedNow.Value()) != 1 {
		test.Fatalf("applied list: %+v / %v", appliedNow.Value(), appliedNow.Error())
	}
	if removed := component.UnapplyService(ctx, session, "ggalmazor.com", available.Value()[0].ID); removed.Failed() {
		test.Fatalf("unapply: %v", removed.Error())
	}
	appliedAfter := component.AppliedServices(ctx, session, "ggalmazor.com")
	if appliedAfter.Failed() || len(appliedAfter.Value()) != 0 {
		test.Fatalf("after unapply: %+v / %v", appliedAfter.Value(), appliedAfter.Error())
	}
}

func TestPushesInitiateListAndResolve(test *testing.T) {
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := newContractServer(test, fake)
	test.Cleanup(server.Close)
	component := pushes.NewComponent(
		NewPushesGateway(NewClient(server.URL, server.Client(), telemetry.NewMetrics())), telemetry.NewMetrics())
	session := identity.Session{Tenant: identity.Tenant{AccountID: 2416}, AccessToken: "token"}
	ctx := context.Background()
	fake.AddDomain("ggalmazor.com", "registered")

	// A bad email fails typed before the API
	var invalid pushes.InvalidPushError
	if outcome := component.InitiatePush(ctx, session, "ggalmazor.com", "not-an-email"); !errors.As(outcome.Error(), &invalid) {
		test.Fatalf("bad email must fail typed, got %v", outcome.Error())
	}

	// A push initiates, lists, and accepts away
	initiated := component.InitiatePush(ctx, session, "ggalmazor.com", "other@example.com")
	if initiated.Failed() || initiated.Value().ID == 0 {
		test.Fatalf("initiate: %+v / %v", initiated.Value(), initiated.Error())
	}
	listed := component.Pushes(ctx, session)
	if listed.Failed() || len(listed.Value()) != 1 {
		test.Fatalf("list: %+v / %v", listed.Value(), listed.Error())
	}
	if accepted := component.AcceptPush(ctx, session, initiated.Value().ID, 7); accepted.Failed() {
		test.Fatalf("accept: %v", accepted.Error())
	}
	remaining := component.Pushes(ctx, session)
	if remaining.Failed() || len(remaining.Value()) != 0 {
		test.Fatalf("after accept: %+v / %v", remaining.Value(), remaining.Error())
	}
}
