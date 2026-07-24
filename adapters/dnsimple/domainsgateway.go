package dnsimple

import (
	"context"
	"fmt"
	"net/http"

	"dnsshimple/core/domains"
	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type DomainsGateway struct {
	client *Client
}

func NewDomainsGateway(client *Client) DomainsGateway {
	return DomainsGateway{client: client}
}

var _ domains.Gateway = DomainsGateway{}

func (gateway DomainsGateway) Domains(ctx context.Context, session identity.Session) kit.Outcome[[]domains.Domain] {
	type domainJSON struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		State     string `json:"state"`
		AutoRenew bool   `json:"auto_renew"`
		ExpiresAt string `json:"expires_at"`
	}
	listed := listAll[domainJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains?per_page=100", session.Tenant.AccountID), "/domains")
	return kit.Chain(listed, func(raw []domainJSON) kit.Outcome[[]domains.Domain] {
		converted := make([]domains.Domain, 0, len(raw))
		for _, domain := range raw {
			converted = append(converted, domains.Domain(domain))
		}
		return kit.Success(converted)
	})
}

func (gateway DomainsGateway) DNSSEC(ctx context.Context, session identity.Session, domainName string) kit.Outcome[domains.DNSSECStatus] {
	type dnssecJSON struct {
		Enabled bool `json:"enabled"`
	}
	// GetFresh: a toggle read back through the 45 s cache would show the
	// old state right after the user flipped it.
	status := decodeData[dnssecJSON](gateway.client.GetFresh(ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains/%s/dnssec", session.Tenant.AccountID, domainName), "/domains/dnssec"))
	return kit.Chain(status, func(raw dnssecJSON) kit.Outcome[domains.DNSSECStatus] {
		return kit.Success(domains.DNSSECStatus(raw))
	})
}

func (gateway DomainsGateway) SetDNSSEC(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing] {
	method := http.MethodPost
	if !enabled {
		method = http.MethodDelete
	}
	changed := gateway.client.Write(ctx, session.AccessToken, method,
		fmt.Sprintf("/%d/domains/%s/dnssec", session.Tenant.AccountID, domainName), "/domains/dnssec", nil)
	return kit.Chain(changed, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

type emailForwardJSON struct {
	ID               int64  `json:"id"`
	AliasEmail       string `json:"alias_email"`
	DestinationEmail string `json:"destination_email"`
}

func toEmailForward(raw emailForwardJSON) domains.EmailForward {
	return domains.EmailForward{ID: raw.ID, From: raw.AliasEmail, To: raw.DestinationEmail}
}

func (gateway DomainsGateway) EmailForwards(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]domains.EmailForward] {
	listed := listAll[emailForwardJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains/%s/email_forwards?per_page=100", session.Tenant.AccountID, domainName),
		"/domains/email_forwards")
	return kit.Chain(listed, func(raw []emailForwardJSON) kit.Outcome[[]domains.EmailForward] {
		converted := make([]domains.EmailForward, 0, len(raw))
		for _, forward := range raw {
			converted = append(converted, toEmailForward(forward))
		}
		return kit.Success(converted)
	})
}

func (gateway DomainsGateway) CreateEmailForward(ctx context.Context, session identity.Session, domainName, alias, destination string) kit.Outcome[domains.EmailForward] {
	created := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/domains/%s/email_forwards", session.Tenant.AccountID, domainName),
		"/domains/email_forwards", map[string]string{"alias_name": alias, "destination_email": destination})
	return kit.Chain(decodeData[emailForwardJSON](created),
		func(raw emailForwardJSON) kit.Outcome[domains.EmailForward] {
			return kit.Success(toEmailForward(raw))
		})
}

func (gateway DomainsGateway) DeleteEmailForward(ctx context.Context, session identity.Session, domainName string, forwardID int64) kit.Outcome[kit.Nothing] {
	deleted := gateway.client.Write(ctx, session.AccessToken, http.MethodDelete,
		fmt.Sprintf("/%d/domains/%s/email_forwards/%d", session.Tenant.AccountID, domainName, forwardID),
		"/domains/email_forwards", nil)
	return kit.Chain(deleted, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

type dsRecordJSON struct {
	ID         int64  `json:"id"`
	KeyTag     string `json:"keytag"`
	Algorithm  string `json:"algorithm"`
	DigestType string `json:"digest_type"`
	Digest     string `json:"digest"`
}

func toDSRecord(raw dsRecordJSON) domains.DSRecord {
	return domains.DSRecord(raw)
}

func (gateway DomainsGateway) DSRecords(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]domains.DSRecord] {
	listed := listAll[dsRecordJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains/%s/ds_records?per_page=100", session.Tenant.AccountID, domainName),
		"/domains/ds_records")
	return kit.Chain(listed, func(raw []dsRecordJSON) kit.Outcome[[]domains.DSRecord] {
		converted := make([]domains.DSRecord, 0, len(raw))
		for _, record := range raw {
			converted = append(converted, toDSRecord(record))
		}
		return kit.Success(converted)
	})
}

func (gateway DomainsGateway) CreateDSRecord(ctx context.Context, session identity.Session, domainName string, record domains.DSRecord) kit.Outcome[domains.DSRecord] {
	created := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/domains/%s/ds_records", session.Tenant.AccountID, domainName),
		"/domains/ds_records", map[string]string{
			"keytag": record.KeyTag, "algorithm": record.Algorithm,
			"digest_type": record.DigestType, "digest": record.Digest,
		})
	return kit.Chain(decodeData[dsRecordJSON](created),
		func(raw dsRecordJSON) kit.Outcome[domains.DSRecord] { return kit.Success(toDSRecord(raw)) })
}

func (gateway DomainsGateway) DeleteDSRecord(ctx context.Context, session identity.Session, domainName string, dsRecordID int64) kit.Outcome[kit.Nothing] {
	deleted := gateway.client.Write(ctx, session.AccessToken, http.MethodDelete,
		fmt.Sprintf("/%d/domains/%s/ds_records/%d", session.Tenant.AccountID, domainName, dsRecordID),
		"/domains/ds_records", nil)
	return kit.Chain(deleted, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}
