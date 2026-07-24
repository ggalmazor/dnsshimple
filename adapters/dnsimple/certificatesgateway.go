package dnsimple

import (
	"context"
	"fmt"
	"net/http"

	"dnsshimple/core/certificates"
	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type CertificatesGateway struct {
	client *Client
}

func NewCertificatesGateway(client *Client) CertificatesGateway {
	return CertificatesGateway{client: client}
}

var _ certificates.Gateway = CertificatesGateway{}

type certificateJSON struct {
	ID         int64  `json:"id"`
	CommonName string `json:"common_name"`
	State      string `json:"state"`
	AutoRenew  bool   `json:"auto_renew"`
	ExpiresAt  string `json:"expires_at"`
}

func toCertificate(raw certificateJSON) certificates.Certificate {
	return certificates.Certificate(raw)
}

func (gateway CertificatesGateway) Certificates(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]certificates.Certificate] {
	listed := listAll[certificateJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains/%s/certificates?per_page=100", session.Tenant.AccountID, domainName),
		"/certificates")
	return kit.Chain(listed, func(raw []certificateJSON) kit.Outcome[[]certificates.Certificate] {
		converted := make([]certificates.Certificate, 0, len(raw))
		for _, certificate := range raw {
			converted = append(converted, toCertificate(certificate))
		}
		return kit.Success(converted)
	})
}

func (gateway CertificatesGateway) PurchaseLetsEncrypt(ctx context.Context, session identity.Session, domainName string, request certificates.IssueRequest) kit.Outcome[certificates.Purchase] {
	type purchaseJSON struct {
		ID    int64  `json:"id"`
		State string `json:"state"`
	}
	payload := map[string]any{"auto_renew": request.AutoRenew}
	if request.Name != "" {
		payload["name"] = request.Name
	}
	if len(request.AlternateNames) > 0 {
		payload["alternate_names"] = request.AlternateNames
	}
	purchased := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/domains/%s/certificates/letsencrypt", session.Tenant.AccountID, domainName),
		"/certificates/letsencrypt", payload)
	return kit.Chain(decodeData[purchaseJSON](purchased),
		func(raw purchaseJSON) kit.Outcome[certificates.Purchase] {
			return kit.Success(certificates.Purchase(raw))
		})
}

func (gateway CertificatesGateway) IssueLetsEncrypt(ctx context.Context, session identity.Session, domainName string, purchaseID int64) kit.Outcome[certificates.Certificate] {
	issued := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/domains/%s/certificates/letsencrypt/%d/issue", session.Tenant.AccountID, domainName, purchaseID),
		"/certificates/letsencrypt/issue", nil)
	return kit.Chain(decodeData[certificateJSON](issued),
		func(raw certificateJSON) kit.Outcome[certificates.Certificate] {
			return kit.Success(toCertificate(raw))
		})
}

func (gateway CertificatesGateway) CertificateState(ctx context.Context, session identity.Session, domainName string, certificateID int64) kit.Outcome[certificates.Certificate] {
	// GetFresh: polls must see the current state, not the 45 s cache.
	polled := decodeData[certificateJSON](gateway.client.GetFresh(ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains/%s/certificates/%d", session.Tenant.AccountID, domainName, certificateID),
		"/certificates"))
	return kit.Chain(polled, func(raw certificateJSON) kit.Outcome[certificates.Certificate] {
		return kit.Success(toCertificate(raw))
	})
}

func (gateway CertificatesGateway) Download(ctx context.Context, session identity.Session, domainName string, certificateID int64) kit.Outcome[string] {
	type downloadJSON struct {
		Server string   `json:"server"`
		Chain  []string `json:"chain"`
	}
	downloaded := decodeData[downloadJSON](gateway.client.GetFresh(ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains/%s/certificates/%d/download", session.Tenant.AccountID, domainName, certificateID),
		"/certificates/download"))
	return kit.Chain(downloaded, func(raw downloadJSON) kit.Outcome[string] {
		pem := raw.Server
		for _, link := range raw.Chain {
			pem += "\n" + link
		}
		return kit.Success(pem)
	})
}

func (gateway CertificatesGateway) PrivateKey(ctx context.Context, session identity.Session, domainName string, certificateID int64) kit.Outcome[string] {
	type keyJSON struct {
		PrivateKey string `json:"private_key"`
	}
	fetched := decodeData[keyJSON](gateway.client.GetFresh(ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains/%s/certificates/%d/private_key", session.Tenant.AccountID, domainName, certificateID),
		"/certificates/private_key"))
	return kit.Chain(fetched, func(raw keyJSON) kit.Outcome[string] {
		return kit.Success(raw.PrivateKey)
	})
}

func (gateway CertificatesGateway) RenewLetsEncrypt(ctx context.Context, session identity.Session, domainName string, certificateID int64, autoRenew bool) kit.Outcome[certificates.Renewal] {
	type renewalJSON struct {
		ID               int64  `json:"id"`
		OldCertificateID int64  `json:"old_certificate_id"`
		NewCertificateID int64  `json:"new_certificate_id"`
		State            string `json:"state"`
	}
	renewed := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/domains/%s/certificates/letsencrypt/%d/renewals", session.Tenant.AccountID, domainName, certificateID),
		"/certificates/letsencrypt/renewals", map[string]any{"auto_renew": autoRenew})
	return kit.Chain(decodeData[renewalJSON](renewed),
		func(raw renewalJSON) kit.Outcome[certificates.Renewal] {
			return kit.Success(certificates.Renewal(raw))
		})
}

func (gateway CertificatesGateway) IssueLetsEncryptRenewal(ctx context.Context, session identity.Session, domainName string, certificateID, renewalID int64) kit.Outcome[certificates.Certificate] {
	issued := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/domains/%s/certificates/letsencrypt/%d/renewals/%d/issue", session.Tenant.AccountID, domainName, certificateID, renewalID),
		"/certificates/letsencrypt/renewals/issue", nil)
	return kit.Chain(decodeData[certificateJSON](issued),
		func(raw certificateJSON) kit.Outcome[certificates.Certificate] {
			return kit.Success(toCertificate(raw))
		})
}
