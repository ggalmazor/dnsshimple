package dnsimple

import (
	"context"
	"net/http/httptest"
	"testing"

	"dnsshimple/core/certificates"
	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
)

func TestALetsEncryptOrderAdvancesToIssued(test *testing.T) {
	// Given a linked session and a domain
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := httptest.NewServer(fake)
	test.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client(), telemetry.NewMetrics())
	gateway := NewCertificatesGateway(client)
	billingGateway := NewBillingGateway(client)
	session := identity.Session{
		Tenant:      identity.Tenant{ID: 1, AccountID: 2416},
		AccessToken: "fake-access-token-2416",
	}
	ctx := context.Background()

	// When the Let's Encrypt order is purchased, issued, and polled twice
	purchase := gateway.PurchaseLetsEncrypt(ctx, session, "ggalmazor.com", certificates.IssueRequest{AutoRenew: true})
	if purchase.Failed() {
		test.Fatalf("purchase: %v", purchase.Error())
	}
	requested := gateway.IssueLetsEncrypt(ctx, session, "ggalmazor.com", purchase.Value().ID)
	if requested.Failed() || requested.Value().Terminal() {
		test.Fatalf("issue must accept as in-flight: %+v / %v", requested.Value(), requested.Error())
	}
	firstPoll := gateway.CertificateState(ctx, session, "ggalmazor.com", requested.Value().ID)
	if firstPoll.Failed() || firstPoll.Value().Terminal() {
		test.Fatalf("first poll must still be in flight: %+v", firstPoll.Value())
	}
	secondPoll := gateway.CertificateState(ctx, session, "ggalmazor.com", requested.Value().ID)

	// Then the certificate reaches issued and shows in the domain's list
	if secondPoll.Failed() || secondPoll.Value().State != certificates.StateIssued {
		test.Fatalf("second poll: %+v / %v", secondPoll.Value(), secondPoll.Error())
	}
	listed := gateway.Certificates(ctx, session, "ggalmazor.com")
	if listed.Failed() || len(listed.Value()) != 1 || listed.Value()[0].State != certificates.StateIssued {
		test.Fatalf("certificates: %+v / %v", listed.Value(), listed.Error())
	}

	// And billing round-trips its charges
	fake.AddCharge("2026-07-01T00:00:00Z", "14.50", "1-1", "collected")
	charges := billingGateway.Charges(ctx, session)
	if charges.Failed() || len(charges.Value()) != 1 || charges.Value()[0].TotalAmount != "14.50" {
		test.Fatalf("charges: %+v / %v", charges.Value(), charges.Error())
	}
}
