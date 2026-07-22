package dnsimple

import (
	"context"
	"fmt"
	"net/http"

	"dnsshimple/core/identity"
	"dnsshimple/core/pushes"
	"dnsshimple/core/services"
	"dnsshimple/kit"
)

type ServicesGateway struct {
	client *Client
}

func NewServicesGateway(client *Client) ServicesGateway {
	return ServicesGateway{client: client}
}

var _ services.Gateway = ServicesGateway{}

type serviceJSON struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	SID         string `json:"sid"`
	Description string `json:"description"`
}

func toServices(raw []serviceJSON) []services.Service {
	converted := make([]services.Service, 0, len(raw))
	for _, service := range raw {
		converted = append(converted, services.Service(service))
	}
	return converted
}

func (gateway ServicesGateway) AvailableServices(ctx context.Context, session identity.Session) kit.Outcome[[]services.Service] {
	listed := listAll[serviceJSON](gateway.client, ctx, session.AccessToken,
		"/services?per_page=100", "/services")
	return kit.Chain(listed, func(raw []serviceJSON) kit.Outcome[[]services.Service] {
		return kit.Success(toServices(raw))
	})
}

func (gateway ServicesGateway) AppliedServices(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]services.Service] {
	listed := listAll[serviceJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains/%s/services?per_page=100", session.Tenant.AccountID, domainName),
		"/domains/services")
	return kit.Chain(listed, func(raw []serviceJSON) kit.Outcome[[]services.Service] {
		return kit.Success(toServices(raw))
	})
}

func (gateway ServicesGateway) ApplyService(ctx context.Context, session identity.Session, domainName string, serviceID int64) kit.Outcome[kit.Nothing] {
	applied := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/domains/%s/services/%d", session.Tenant.AccountID, domainName, serviceID),
		"/domains/services", nil)
	return kit.Chain(applied, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway ServicesGateway) UnapplyService(ctx context.Context, session identity.Session, domainName string, serviceID int64) kit.Outcome[kit.Nothing] {
	unapplied := gateway.client.Write(ctx, session.AccessToken, http.MethodDelete,
		fmt.Sprintf("/%d/domains/%s/services/%d", session.Tenant.AccountID, domainName, serviceID),
		"/domains/services", nil)
	return kit.Chain(unapplied, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

type PushesGateway struct {
	client *Client
}

func NewPushesGateway(client *Client) PushesGateway {
	return PushesGateway{client: client}
}

var _ pushes.Gateway = PushesGateway{}

type pushJSON struct {
	ID        int64  `json:"id"`
	DomainID  int64  `json:"domain_id"`
	CreatedAt string `json:"created_at"`
}

func (gateway PushesGateway) InitiatePush(ctx context.Context, session identity.Session, domainName, newAccountEmail string) kit.Outcome[pushes.Push] {
	initiated := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/domains/%s/pushes", session.Tenant.AccountID, domainName),
		"/domains/pushes", map[string]string{"new_account_email": newAccountEmail})
	return kit.Chain(decodeData[pushJSON](initiated),
		func(raw pushJSON) kit.Outcome[pushes.Push] { return kit.Success(pushes.Push(raw)) })
}

func (gateway PushesGateway) Pushes(ctx context.Context, session identity.Session) kit.Outcome[[]pushes.Push] {
	listed := listAll[pushJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/pushes?per_page=100", session.Tenant.AccountID), "/pushes")
	return kit.Chain(listed, func(raw []pushJSON) kit.Outcome[[]pushes.Push] {
		converted := make([]pushes.Push, 0, len(raw))
		for _, push := range raw {
			converted = append(converted, pushes.Push(push))
		}
		return kit.Success(converted)
	})
}

func (gateway PushesGateway) AcceptPush(ctx context.Context, session identity.Session, pushID, contactID int64) kit.Outcome[kit.Nothing] {
	accepted := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/pushes/%d", session.Tenant.AccountID, pushID),
		"/pushes", map[string]int64{"contact_id": contactID})
	return kit.Chain(accepted, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway PushesGateway) RejectPush(ctx context.Context, session identity.Session, pushID int64) kit.Outcome[kit.Nothing] {
	rejected := gateway.client.Write(ctx, session.AccessToken, http.MethodDelete,
		fmt.Sprintf("/%d/pushes/%d", session.Tenant.AccountID, pushID), "/pushes", nil)
	return kit.Chain(rejected, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}
