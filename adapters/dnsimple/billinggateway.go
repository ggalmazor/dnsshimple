package dnsimple

import (
	"context"
	"fmt"

	"dnsshimple/core/billing"
	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type BillingGateway struct {
	client *Client
}

func NewBillingGateway(client *Client) BillingGateway {
	return BillingGateway{client: client}
}

var _ billing.Gateway = BillingGateway{}

func (gateway BillingGateway) Charges(ctx context.Context, session identity.Session) kit.Outcome[[]billing.Charge] {
	type chargeJSON struct {
		InvoicedAt    string `json:"invoiced_at"`
		TotalAmount   string `json:"total_amount"`
		BalanceAmount string `json:"balance_amount"`
		Reference     string `json:"reference"`
		State         string `json:"state"`
	}
	listed := listAll[chargeJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/billing/charges?per_page=100", session.Tenant.AccountID), "/billing/charges")
	return kit.Chain(listed, func(raw []chargeJSON) kit.Outcome[[]billing.Charge] {
		converted := make([]billing.Charge, 0, len(raw))
		for _, charge := range raw {
			converted = append(converted, billing.Charge(charge))
		}
		return kit.Success(converted)
	})
}
