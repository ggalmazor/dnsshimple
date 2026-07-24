package dnsimple

import (
	"context"
	"fmt"

	"dnsshimple/core/contacts"
	"dnsshimple/core/domains"
	"dnsshimple/core/identity"
	"dnsshimple/core/insights"
	"dnsshimple/core/registrar"
	"dnsshimple/core/secondarydns"
	"dnsshimple/core/services"
	"dnsshimple/core/templates"
	"dnsshimple/core/webhooks"
	"dnsshimple/core/zones"
	"dnsshimple/kit"
)

// This file covers the API's single-item show operations. A list-driven TUI
// rarely needs them, so they live at the adapter level - callable, fake-
// backed, and contract-checked - without widening the core ports.

// Account is one entry of GET /accounts: every account the token can act
// for (one, for our per-account OAuth tokens).
type Account struct {
	ID             int64  `json:"id"`
	Email          string `json:"email"`
	PlanIdentifier string `json:"plan_identifier"`
}

type AccountsGateway struct {
	client *Client
}

func NewAccountsGateway(client *Client) AccountsGateway {
	return AccountsGateway{client: client}
}

func (gateway AccountsGateway) Accounts(ctx context.Context, accessToken string) kit.Outcome[[]Account] {
	return decodeData[[]Account](gateway.client.Get(ctx, accessToken, "/accounts", "/accounts"))
}

// DomainResearch is the registry's answer about a name outside the account.
type DomainResearch struct {
	RequestID    string   `json:"request_id"`
	Domain       string   `json:"domain"`
	Availability string   `json:"availability"`
	Errors       []string `json:"errors"`
}

func (gateway DomainsGateway) ResearchDomainStatus(ctx context.Context, session identity.Session, domainName string) kit.Outcome[DomainResearch] {
	return decodeData[DomainResearch](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains/research/status?domain=%s", session.Tenant.AccountID, domainName),
		"/domains/research"))
}

func (gateway ContactsGateway) Contact(ctx context.Context, session identity.Session, contactID int64) kit.Outcome[contacts.Contact] {
	shown := decodeData[fullContactJSON](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/%d/contacts/%d", session.Tenant.AccountID, contactID), "/contacts"))
	return kit.Chain(shown, func(raw fullContactJSON) kit.Outcome[contacts.Contact] {
		return kit.Success(toContact(raw))
	})
}

func (gateway DomainsGateway) DSRecord(ctx context.Context, session identity.Session, domainName string, dsRecordID int64) kit.Outcome[domains.DSRecord] {
	shown := decodeData[dsRecordJSON](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains/%s/ds_records/%d", session.Tenant.AccountID, domainName, dsRecordID),
		"/domains/ds_records"))
	return kit.Chain(shown, func(raw dsRecordJSON) kit.Outcome[domains.DSRecord] {
		return kit.Success(toDSRecord(raw))
	})
}

func (gateway DomainsGateway) EmailForward(ctx context.Context, session identity.Session, domainName string, forwardID int64) kit.Outcome[domains.EmailForward] {
	shown := decodeData[emailForwardJSON](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains/%s/email_forwards/%d", session.Tenant.AccountID, domainName, forwardID),
		"/domains/email_forwards"))
	return kit.Chain(shown, func(raw emailForwardJSON) kit.Outcome[domains.EmailForward] {
		return kit.Success(toEmailForward(raw))
	})
}

func (gateway RegistrarGateway) RenewalState(ctx context.Context, session identity.Session, domainName string, renewalID int64) kit.Outcome[registrar.Renewal] {
	return decodeData[registrar.Renewal](gateway.client.GetFresh(ctx, session.AccessToken,
		fmt.Sprintf("/%d/registrar/domains/%s/renewals/%d", session.Tenant.AccountID, domainName, renewalID),
		"/registrar/renewals"))
}

func (gateway RegistrarGateway) RestoreState(ctx context.Context, session identity.Session, domainName string, restoreID int64) kit.Outcome[registrar.Restore] {
	return decodeData[registrar.Restore](gateway.client.GetFresh(ctx, session.AccessToken,
		fmt.Sprintf("/%d/registrar/domains/%s/restores/%d", session.Tenant.AccountID, domainName, restoreID),
		"/registrar/restores"))
}

func (gateway SecondaryDNSGateway) Primary(ctx context.Context, session identity.Session, primaryID int64) kit.Outcome[secondarydns.PrimaryServer] {
	shown := decodeData[primaryJSON](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/%d/secondary_dns/primaries/%d", session.Tenant.AccountID, primaryID),
		"/secondary_dns/primaries"))
	return kit.Chain(shown, func(raw primaryJSON) kit.Outcome[secondarydns.PrimaryServer] {
		return kit.Success(toPrimary(raw))
	})
}

func (gateway TemplatesGateway) Template(ctx context.Context, session identity.Session, templateID int64) kit.Outcome[templates.Template] {
	shown := decodeData[templateJSON](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/%d/templates/%d", session.Tenant.AccountID, templateID), "/templates"))
	return kit.Chain(shown, func(raw templateJSON) kit.Outcome[templates.Template] {
		return kit.Success(templates.Template(raw))
	})
}

func (gateway TemplatesGateway) TemplateRecord(ctx context.Context, session identity.Session, templateID, recordID int64) kit.Outcome[zones.Record] {
	shown := decodeData[recordJSON](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/%d/templates/%d/records/%d", session.Tenant.AccountID, templateID, recordID),
		"/templates/records"))
	return kit.Chain(shown, func(raw recordJSON) kit.Outcome[zones.Record] {
		return kit.Success(toRecord(raw))
	})
}

func (gateway WebhooksGateway) Webhook(ctx context.Context, session identity.Session, webhookID int64) kit.Outcome[webhooks.Webhook] {
	shown := decodeData[webhookJSON](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/%d/webhooks/%d", session.Tenant.AccountID, webhookID), "/webhooks"))
	return kit.Chain(shown, func(raw webhookJSON) kit.Outcome[webhooks.Webhook] {
		return kit.Success(webhooks.Webhook(raw))
	})
}

func (gateway ZonesGateway) Zone(ctx context.Context, session identity.Session, zoneName string) kit.Outcome[zones.Zone] {
	type zoneShowJSON struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Active bool   `json:"active"`
	}
	shown := decodeData[zoneShowJSON](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/%d/zones/%s", session.Tenant.AccountID, zoneName), "/zones"))
	return kit.Chain(shown, func(raw zoneShowJSON) kit.Outcome[zones.Zone] {
		return kit.Success(zones.Zone{Name: raw.Name, Active: raw.Active})
	})
}

func (gateway ZonesGateway) Record(ctx context.Context, session identity.Session, zoneName string, recordID int64) kit.Outcome[zones.Record] {
	shown := decodeData[recordJSON](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/%d/zones/%s/records/%d", session.Tenant.AccountID, zoneName, recordID),
		"/zones/records"))
	return kit.Chain(shown, func(raw recordJSON) kit.Outcome[zones.Record] {
		return kit.Success(toRecord(raw))
	})
}

func (gateway InsightsGateway) TLD(ctx context.Context, session identity.Session, tld string) kit.Outcome[insights.TLD] {
	type tldJSON struct {
		TLD                 string `json:"tld"`
		RegistrationEnabled bool   `json:"registration_enabled"`
		TransferEnabled     bool   `json:"transfer_enabled"`
		WhoisPrivacy        bool   `json:"whois_privacy"`
		IDN                 bool   `json:"idn"`
		MinimumRegistration int    `json:"minimum_registration"`
	}
	shown := decodeData[tldJSON](gateway.client.Get(ctx, session.AccessToken,
		"/tlds/"+tld, "/tlds"))
	return kit.Chain(shown, func(raw tldJSON) kit.Outcome[insights.TLD] {
		return kit.Success(insights.TLD{
			Name: raw.TLD, RegistrationEnabled: raw.RegistrationEnabled,
			TransferEnabled: raw.TransferEnabled, WhoisPrivacy: raw.WhoisPrivacy,
			IDN: raw.IDN, MinimumYears: raw.MinimumRegistration,
		})
	})
}

func (gateway ServicesGateway) Service(ctx context.Context, session identity.Session, serviceID int64) kit.Outcome[services.Service] {
	shown := decodeData[serviceJSON](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/services/%d", serviceID), "/services"))
	return kit.Chain(shown, func(raw serviceJSON) kit.Outcome[services.Service] {
		return kit.Success(services.Service(raw))
	})
}
