package dnsimple

import (
	"context"
	"fmt"
	"net/http"

	"dnsshimple/core/identity"
	"dnsshimple/core/insights"
	"dnsshimple/core/secondarydns"
	"dnsshimple/kit"
)

type InsightsGateway struct {
	client *Client
}

func NewInsightsGateway(client *Client) InsightsGateway {
	return InsightsGateway{client: client}
}

var _ insights.Gateway = InsightsGateway{}

func (gateway InsightsGateway) VolumeByZone(ctx context.Context, session identity.Session) kit.Outcome[[]insights.VolumeRow] {
	type rowJSON struct {
		ZoneName string `json:"zone_name"`
		Volume   int64  `json:"volume"`
	}
	listed := listAll[rowJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/dns_analytics?groupings=zone_name&per_page=100", session.Tenant.AccountID),
		"/dns_analytics")
	return kit.Chain(listed, func(raw []rowJSON) kit.Outcome[[]insights.VolumeRow] {
		converted := make([]insights.VolumeRow, 0, len(raw))
		for _, row := range raw {
			converted = append(converted, insights.VolumeRow(row))
		}
		return kit.Success(converted)
	})
}

func (gateway InsightsGateway) TLDs(ctx context.Context, session identity.Session) kit.Outcome[[]insights.TLD] {
	type tldJSON struct {
		TLD                 string `json:"tld"`
		RegistrationEnabled bool   `json:"registration_enabled"`
		TransferEnabled     bool   `json:"transfer_enabled"`
		WhoisPrivacy        bool   `json:"whois_privacy"`
		IDN                 bool   `json:"idn"`
		MinimumRegistration int    `json:"minimum_registration"`
	}
	listed := listAll[tldJSON](gateway.client, ctx, session.AccessToken,
		"/tlds?per_page=100", "/tlds")
	return kit.Chain(listed, func(raw []tldJSON) kit.Outcome[[]insights.TLD] {
		converted := make([]insights.TLD, 0, len(raw))
		for _, tld := range raw {
			converted = append(converted, insights.TLD{
				Name: tld.TLD, RegistrationEnabled: tld.RegistrationEnabled,
				TransferEnabled: tld.TransferEnabled, WhoisPrivacy: tld.WhoisPrivacy,
				IDN: tld.IDN, MinimumYears: tld.MinimumRegistration,
			})
		}
		return kit.Success(converted)
	})
}

type SecondaryDNSGateway struct {
	client *Client
}

func NewSecondaryDNSGateway(client *Client) SecondaryDNSGateway {
	return SecondaryDNSGateway{client: client}
}

var _ secondarydns.Gateway = SecondaryDNSGateway{}

type primaryJSON struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	LinkedAt string `json:"linked_at"`
}

func toPrimary(raw primaryJSON) secondarydns.PrimaryServer {
	return secondarydns.PrimaryServer{
		ID: raw.ID, Name: raw.Name, IP: raw.IP, Port: raw.Port,
		Linked: raw.LinkedAt != "",
	}
}

func (gateway SecondaryDNSGateway) Primaries(ctx context.Context, session identity.Session) kit.Outcome[[]secondarydns.PrimaryServer] {
	listed := listAll[primaryJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/secondary_dns/primaries?per_page=100", session.Tenant.AccountID),
		"/secondary_dns/primaries")
	return kit.Chain(listed, func(raw []primaryJSON) kit.Outcome[[]secondarydns.PrimaryServer] {
		converted := make([]secondarydns.PrimaryServer, 0, len(raw))
		for _, primary := range raw {
			converted = append(converted, toPrimary(primary))
		}
		return kit.Success(converted)
	})
}

func (gateway SecondaryDNSGateway) CreatePrimary(ctx context.Context, session identity.Session, name, ip string, port int) kit.Outcome[secondarydns.PrimaryServer] {
	created := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/secondary_dns/primaries", session.Tenant.AccountID),
		"/secondary_dns/primaries", map[string]any{"name": name, "ip": ip, "port": port})
	return kit.Chain(decodeData[primaryJSON](created),
		func(raw primaryJSON) kit.Outcome[secondarydns.PrimaryServer] { return kit.Success(toPrimary(raw)) })
}

func (gateway SecondaryDNSGateway) DeletePrimary(ctx context.Context, session identity.Session, primaryID int64) kit.Outcome[kit.Nothing] {
	deleted := gateway.client.Write(ctx, session.AccessToken, http.MethodDelete,
		fmt.Sprintf("/%d/secondary_dns/primaries/%d", session.Tenant.AccountID, primaryID),
		"/secondary_dns/primaries", nil)
	return kit.Chain(deleted, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway SecondaryDNSGateway) SetPrimaryLink(ctx context.Context, session identity.Session, primaryID int64, linked bool) kit.Outcome[kit.Nothing] {
	action := "link"
	if !linked {
		action = "unlink"
	}
	changed := gateway.client.Write(ctx, session.AccessToken, http.MethodPut,
		fmt.Sprintf("/%d/secondary_dns/primaries/%d/%s", session.Tenant.AccountID, primaryID, action),
		"/secondary_dns/primaries", nil)
	return kit.Chain(changed, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway SecondaryDNSGateway) SecondaryZones(ctx context.Context, session identity.Session) kit.Outcome[[]string] {
	type secondaryZoneJSON struct {
		Name string `json:"name"`
	}
	listed := listAll[secondaryZoneJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/secondary_dns/zones?per_page=100", session.Tenant.AccountID),
		"/secondary_dns/zones")
	return kit.Chain(listed, func(raw []secondaryZoneJSON) kit.Outcome[[]string] {
		names := make([]string, 0, len(raw))
		for _, zone := range raw {
			names = append(names, zone.Name)
		}
		return kit.Success(names)
	})
}

func (gateway SecondaryDNSGateway) CreateSecondaryZone(ctx context.Context, session identity.Session, zoneName string) kit.Outcome[kit.Nothing] {
	created := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/secondary_dns/zones", session.Tenant.AccountID),
		"/secondary_dns/zones", map[string]string{"name": zoneName})
	return kit.Chain(created, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}
