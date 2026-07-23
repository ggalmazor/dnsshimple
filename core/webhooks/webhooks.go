// Package webhooks is the Component for account webhooks (DESIGN.md §3.1,
// M8): HTTP endpoints DNSimple notifies about account events.
package webhooks

import (
	"context"
	"log/slog"
	"regexp"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type Webhook struct {
	ID  int64
	URL string
}

type InvalidWebhookError struct {
	Reason string
}

func (invalid InvalidWebhookError) Error() string {
	return "webhooks: invalid webhook: " + invalid.Reason
}

var urlPattern = regexp.MustCompile(`^https://[^\s]+$`)

type Gateway interface {
	Webhooks(ctx context.Context, session identity.Session) kit.Outcome[[]Webhook]
	CreateWebhook(ctx context.Context, session identity.Session, url string) kit.Outcome[Webhook]
	DeleteWebhook(ctx context.Context, session identity.Session, webhookID int64) kit.Outcome[kit.Nothing]
}

type Component struct {
	gateway Gateway
}

func NewComponent(gateway Gateway) *Component {
	return &Component{gateway: gateway}
}

func (component *Component) Webhooks(ctx context.Context, session identity.Session) kit.Outcome[[]Webhook] {
	return component.gateway.Webhooks(ctx, session)
}

// CreateWebhook demands https: DNSimple posts account events to this URL,
// and account events over plain HTTP are a leak, not a webhook.
func (component *Component) CreateWebhook(ctx context.Context, session identity.Session, url string) kit.Outcome[Webhook] {
	if !urlPattern.MatchString(url) {
		return kit.Failure[Webhook](InvalidWebhookError{Reason: "the URL must be https:// with no spaces"})
	}
	created := component.gateway.CreateWebhook(ctx, session, url)
	if !created.Failed() {
		slog.InfoContext(ctx, "webhook created", "webhook_id", created.Value().ID)
	}
	return created
}

func (component *Component) DeleteWebhook(ctx context.Context, session identity.Session, webhookID int64) kit.Outcome[kit.Nothing] {
	return component.gateway.DeleteWebhook(ctx, session, webhookID)
}
