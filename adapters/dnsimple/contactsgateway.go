package dnsimple

import (
	"context"
	"fmt"
	"net/http"

	"dnsshimple/core/contacts"
	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type ContactsGateway struct {
	client *Client
}

func NewContactsGateway(client *Client) ContactsGateway {
	return ContactsGateway{client: client}
}

var _ contacts.Gateway = ContactsGateway{}

type fullContactJSON struct {
	ID            int64  `json:"id"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	Address       string `json:"address1"`
	City          string `json:"city"`
	StateProvince string `json:"state_province"`
	PostalCode    string `json:"postal_code"`
	Country       string `json:"country"`
}

func toContact(raw fullContactJSON) contacts.Contact {
	return contacts.Contact(raw)
}

func contactPayload(draft contacts.Draft) map[string]string {
	return map[string]string{
		"first_name":     draft.FirstName,
		"last_name":      draft.LastName,
		"email":          draft.Email,
		"phone":          draft.Phone,
		"address1":       draft.Address,
		"city":           draft.City,
		"state_province": draft.StateProvince,
		"postal_code":    draft.PostalCode,
		"country":        draft.Country,
	}
}

func (gateway ContactsGateway) Contacts(ctx context.Context, session identity.Session) kit.Outcome[[]contacts.Contact] {
	listed := listAll[fullContactJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/contacts?per_page=100", session.Tenant.AccountID), "/contacts")
	return kit.Chain(listed, func(raw []fullContactJSON) kit.Outcome[[]contacts.Contact] {
		converted := make([]contacts.Contact, 0, len(raw))
		for _, contact := range raw {
			converted = append(converted, toContact(contact))
		}
		return kit.Success(converted)
	})
}

func (gateway ContactsGateway) CreateContact(ctx context.Context, session identity.Session, draft contacts.Draft) kit.Outcome[contacts.Contact] {
	created := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/contacts", session.Tenant.AccountID), "/contacts", contactPayload(draft))
	return kit.Chain(decodeData[fullContactJSON](created),
		func(raw fullContactJSON) kit.Outcome[contacts.Contact] { return kit.Success(toContact(raw)) })
}

func (gateway ContactsGateway) UpdateContact(ctx context.Context, session identity.Session, contactID int64, draft contacts.Draft) kit.Outcome[contacts.Contact] {
	updated := gateway.client.Write(ctx, session.AccessToken, http.MethodPatch,
		fmt.Sprintf("/%d/contacts/%d", session.Tenant.AccountID, contactID), "/contacts", contactPayload(draft))
	return kit.Chain(decodeData[fullContactJSON](updated),
		func(raw fullContactJSON) kit.Outcome[contacts.Contact] { return kit.Success(toContact(raw)) })
}

func (gateway ContactsGateway) DeleteContact(ctx context.Context, session identity.Session, contactID int64) kit.Outcome[kit.Nothing] {
	deleted := gateway.client.Write(ctx, session.AccessToken, http.MethodDelete,
		fmt.Sprintf("/%d/contacts/%d", session.Tenant.AccountID, contactID), "/contacts", nil)
	return kit.Chain(deleted, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}
