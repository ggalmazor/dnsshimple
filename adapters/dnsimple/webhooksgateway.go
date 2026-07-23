package dnsimple

import (
	"context"
	"fmt"
	"net/http"

	"dnsshimple/core/identity"
	"dnsshimple/core/webhooks"
	"dnsshimple/kit"
)

type WebhooksGateway struct {
	client *Client
}

func NewWebhooksGateway(client *Client) WebhooksGateway {
	return WebhooksGateway{client: client}
}

var _ webhooks.Gateway = WebhooksGateway{}

type webhookJSON struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
}

func (gateway WebhooksGateway) Webhooks(ctx context.Context, session identity.Session) kit.Outcome[[]webhooks.Webhook] {
	listed := listAll[webhookJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/webhooks?per_page=100", session.Tenant.AccountID), "/webhooks")
	return kit.Chain(listed, func(raw []webhookJSON) kit.Outcome[[]webhooks.Webhook] {
		converted := make([]webhooks.Webhook, 0, len(raw))
		for _, webhook := range raw {
			converted = append(converted, webhooks.Webhook(webhook))
		}
		return kit.Success(converted)
	})
}

func (gateway WebhooksGateway) CreateWebhook(ctx context.Context, session identity.Session, url string) kit.Outcome[webhooks.Webhook] {
	created := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/webhooks", session.Tenant.AccountID), "/webhooks", map[string]string{"url": url})
	return kit.Chain(decodeData[webhookJSON](created),
		func(raw webhookJSON) kit.Outcome[webhooks.Webhook] { return kit.Success(webhooks.Webhook(raw)) })
}

func (gateway WebhooksGateway) DeleteWebhook(ctx context.Context, session identity.Session, webhookID int64) kit.Outcome[kit.Nothing] {
	deleted := gateway.client.Write(ctx, session.AccessToken, http.MethodDelete,
		fmt.Sprintf("/%d/webhooks/%d", session.Tenant.AccountID, webhookID), "/webhooks", nil)
	return kit.Chain(deleted, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}
