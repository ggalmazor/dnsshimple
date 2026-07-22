package dnsimple

import (
	"context"
	"fmt"

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
	listed := decodeData[[]domainJSON](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains?per_page=100", session.Tenant.AccountID), "/domains"))
	return kit.Chain(listed, func(raw []domainJSON) kit.Outcome[[]domains.Domain] {
		converted := make([]domains.Domain, 0, len(raw))
		for _, domain := range raw {
			converted = append(converted, domains.Domain(domain))
		}
		return kit.Success(converted)
	})
}
