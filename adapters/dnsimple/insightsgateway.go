package dnsimple

import (
	"context"
	"encoding/json"
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

// VolumeByZone walks the analytics pages by hand: unlike every other list
// endpoint, /dns_analytics wraps data as an object of headers plus rows of
// values, so the shared pagination walker cannot decode it.
func (gateway InsightsGateway) VolumeByZone(ctx context.Context, session identity.Session) kit.Outcome[[]insights.VolumeRow] {
	var collected []insights.VolumeRow
	for page := 1; ; page++ {
		body := gateway.client.Get(ctx, session.AccessToken,
			fmt.Sprintf("/%d/dns_analytics?groupings=zone_name&per_page=100&page=%d", session.Tenant.AccountID, page),
			"/dns_analytics")
		if body.Failed() {
			return kit.Failure[[]insights.VolumeRow](body.Error())
		}
		var envelope struct {
			Data struct {
				Headers []string `json:"headers"`
				Rows    [][]any  `json:"rows"`
			} `json:"data"`
			Pagination struct {
				TotalPages int `json:"total_pages"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(body.Value(), &envelope); err != nil {
			return kit.Failure[[]insights.VolumeRow](fmt.Errorf("decoding the /dns_analytics page: %w", err))
		}
		zoneColumn, volumeColumn := -1, -1
		for index, header := range envelope.Data.Headers {
			switch header {
			case "zone_name":
				zoneColumn = index
			case "volume":
				volumeColumn = index
			}
		}
		if zoneColumn < 0 || volumeColumn < 0 {
			return kit.Failure[[]insights.VolumeRow](fmt.Errorf(
				"the /dns_analytics response carries no zone_name/volume columns (headers: %v)", envelope.Data.Headers))
		}
		for _, row := range envelope.Data.Rows {
			if len(row) <= zoneColumn || len(row) <= volumeColumn {
				continue
			}
			zoneName, _ := row[zoneColumn].(string)
			volume, _ := row[volumeColumn].(float64)
			collected = append(collected, insights.VolumeRow{ZoneName: zoneName, Volume: int64(volume)})
		}
		if page >= envelope.Pagination.TotalPages {
			return kit.Success(collected)
		}
	}
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
	// Production has no GET on /secondary_dns/zones (only the create); a
	// secondary zone is a Zone with secondary:true, so the list comes from
	// the zones index.
	type zoneJSON struct {
		Name      string `json:"name"`
		Secondary bool   `json:"secondary"`
	}
	listed := listAll[zoneJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/zones?per_page=100", session.Tenant.AccountID),
		"/zones")
	return kit.Chain(listed, func(raw []zoneJSON) kit.Outcome[[]string] {
		names := make([]string, 0, len(raw))
		for _, zone := range raw {
			if zone.Secondary {
				names = append(names, zone.Name)
			}
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
