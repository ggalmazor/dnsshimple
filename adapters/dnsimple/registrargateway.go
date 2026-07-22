package dnsimple

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"dnsshimple/core/identity"
	"dnsshimple/core/registrar"
	"dnsshimple/kit"
)

type RegistrarGateway struct {
	client *Client
}

func NewRegistrarGateway(client *Client) RegistrarGateway {
	return RegistrarGateway{client: client}
}

var _ registrar.Gateway = RegistrarGateway{}

func (gateway RegistrarGateway) CheckAvailability(ctx context.Context, session identity.Session, domainName string) kit.Outcome[registrar.Availability] {
	type checkJSON struct {
		Domain    string `json:"domain"`
		Available bool   `json:"available"`
		Premium   bool   `json:"premium"`
	}
	checked := decodeData[checkJSON](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/%d/registrar/domains/%s/check", session.Tenant.AccountID, domainName),
		"/registrar/check"))
	return kit.Chain(checked, func(raw checkJSON) kit.Outcome[registrar.Availability] {
		return kit.Success(registrar.Availability(raw))
	})
}

func (gateway RegistrarGateway) Contacts(ctx context.Context, session identity.Session) kit.Outcome[[]registrar.Contact] {
	type contactJSON struct {
		ID        int64  `json:"id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Email     string `json:"email"`
	}
	listed := listAll[contactJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/contacts?per_page=100", session.Tenant.AccountID), "/contacts")
	return kit.Chain(listed, func(raw []contactJSON) kit.Outcome[[]registrar.Contact] {
		converted := make([]registrar.Contact, 0, len(raw))
		for _, contact := range raw {
			label := strings.TrimSpace(contact.FirstName + " " + contact.LastName)
			if contact.Email != "" {
				label += " <" + contact.Email + ">"
			}
			converted = append(converted, registrar.Contact{ID: contact.ID, Label: strings.TrimSpace(label)})
		}
		return kit.Success(converted)
	})
}

type registrationJSON struct {
	ID    int64  `json:"id"`
	State string `json:"state"`
}

func (gateway RegistrarGateway) Register(ctx context.Context, session identity.Session, domainName string, contactID int64) kit.Outcome[registrar.Registration] {
	accepted := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/registrar/domains/%s/registrations", session.Tenant.AccountID, domainName),
		"/registrar/registrations", map[string]any{"registrant_id": contactID})
	return kit.Chain(decodeData[registrationJSON](accepted),
		func(raw registrationJSON) kit.Outcome[registrar.Registration] {
			return kit.Success(registrar.Registration(raw))
		})
}

func (gateway RegistrarGateway) SetAutoRenewal(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing] {
	method := http.MethodPut
	if !enabled {
		method = http.MethodDelete
	}
	changed := gateway.client.Write(ctx, session.AccessToken, method,
		fmt.Sprintf("/%d/registrar/domains/%s/auto_renewal", session.Tenant.AccountID, domainName),
		"/registrar/auto_renewal", nil)
	return kit.Chain(changed, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway RegistrarGateway) RegistrationState(ctx context.Context, session identity.Session, domainName string, registrationID int64) kit.Outcome[registrar.Registration] {
	polled := decodeData[registrationJSON](gateway.client.GetFresh(ctx, session.AccessToken,
		fmt.Sprintf("/%d/registrar/domains/%s/registrations/%d", session.Tenant.AccountID, domainName, registrationID),
		"/registrar/registrations"))
	return kit.Chain(polled, func(raw registrationJSON) kit.Outcome[registrar.Registration] {
		return kit.Success(registrar.Registration(raw))
	})
}
