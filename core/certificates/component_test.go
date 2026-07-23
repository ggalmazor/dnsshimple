package certificates

import (
	"context"
	"errors"
	"testing"

	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
	"dnsshimple/kit"
)

func TestIssueCertificatePurchasesIssuesAndQueuesThePoll(test *testing.T) {
	// Given a domain eligible for a Let's Encrypt certificate
	gateway := &fakeGateway{}
	queue := &fakeQueue{}
	component := NewComponent(gateway, queue, telemetry.NewMetrics())
	session := identity.Session{Tenant: identity.Tenant{ID: 42}}

	// When issuance is requested
	outcome := component.IssueCertificate(context.Background(), session, "ggalmazor.com", IssueRequest{Name: "www", AutoRenew: true})

	// Then the certificate is requested from the accepted purchase and the
	// poll is queued for the tenant
	if outcome.Failed() {
		test.Fatalf("IssueCertificate failed: %v", outcome.Error())
	}
	if gateway.issuedPurchase != 900 {
		test.Fatalf("issued from purchase %d, expected 900", gateway.issuedPurchase)
	}
	if !gateway.purchasedAutoRenew {
		test.Fatal("auto_renew was not forwarded")
	}
	if queue.tenantID != 42 || queue.domainName != "ggalmazor.com" || queue.certificateID != outcome.Value().ID {
		test.Fatalf("poll queued wrong: %+v", queue)
	}
}

func TestIssueCertificateStopsAtAFailedPurchase(test *testing.T) {
	// Given a DNSimple that rejects the purchase
	gateway := &fakeGateway{failPurchase: true}
	queue := &fakeQueue{}
	component := NewComponent(gateway, queue, telemetry.NewMetrics())

	// When issuance is requested
	outcome := component.IssueCertificate(context.Background(), identity.Session{}, "ggalmazor.com", IssueRequest{})

	// Then the railway short-circuits: nothing issued, nothing queued
	if !outcome.Failed() {
		test.Fatal("a failed purchase must fail the BTX")
	}
	if gateway.issuedPurchase != 0 || queue.certificateID != 0 {
		test.Fatalf("work happened after the failure: %+v / %+v", gateway, queue)
	}
}

type fakeGateway struct {
	failPurchase       bool
	purchasedAutoRenew bool
	issuedPurchase     int64
}

func (gateway *fakeGateway) Certificates(context.Context, identity.Session, string) kit.Outcome[[]Certificate] {
	return kit.Success([]Certificate{})
}

func (gateway *fakeGateway) PurchaseLetsEncrypt(_ context.Context, _ identity.Session, _ string, request IssueRequest) kit.Outcome[Purchase] {
	if gateway.failPurchase {
		return kit.Failure[Purchase](errors.New("purchase rejected"))
	}
	gateway.purchasedAutoRenew = request.AutoRenew
	return kit.Success(Purchase{ID: 900, State: "new"})
}

func (gateway *fakeGateway) IssueLetsEncrypt(_ context.Context, _ identity.Session, _ string, purchaseID int64) kit.Outcome[Certificate] {
	gateway.issuedPurchase = purchaseID
	return kit.Success(Certificate{ID: 901, CommonName: "ggalmazor.com", State: "requesting"})
}

func (gateway *fakeGateway) CertificateState(context.Context, identity.Session, string, int64) kit.Outcome[Certificate] {
	return kit.Success(Certificate{ID: 901, State: StateIssued})
}

type fakeQueue struct {
	tenantID      int64
	domainName    string
	certificateID int64
}

func (queue *fakeQueue) EnqueueCertificatePoll(_ context.Context, tenantID int64, domainName string, certificateID int64) kit.Outcome[kit.Nothing] {
	queue.tenantID, queue.domainName, queue.certificateID = tenantID, domainName, certificateID
	return kit.Done()
}

func (gateway *fakeGateway) Download(context.Context, identity.Session, string, int64) kit.Outcome[string] {
	return kit.Success("-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----")
}

func (gateway *fakeGateway) PrivateKey(context.Context, identity.Session, string, int64) kit.Outcome[string] {
	return kit.Success("-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----")
}
