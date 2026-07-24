package dnsimple

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"dnsshimple/core/gateway"
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

// disabledWhenAbsent translates DNSimple's 404 on never-purchased features
// (WHOIS privacy on a fresh domain answers Not Found, not enabled:false)
// into the state it means: disabled.
func disabledWhenAbsent(outcome kit.Outcome[bool]) kit.Outcome[bool] {
	var rejection gateway.UpstreamRejectionError
	if outcome.Failed() && errors.As(outcome.Error(), &rejection) && rejection.StatusCode == http.StatusNotFound {
		return kit.Success(false)
	}
	return outcome
}

func (gateway RegistrarGateway) WhoisPrivacy(ctx context.Context, session identity.Session, domainName string) kit.Outcome[bool] {
	// Production offers no GET on whois_privacy (only PUT/DELETE, per the
	// OpenAPI spec and the app's route table); the state lives on the
	// Domain itself as private_whois. GetFresh: toggles read back
	// immediately after flipping.
	type domainJSON struct {
		PrivateWhois bool `json:"private_whois"`
	}
	status := decodeData[domainJSON](gateway.client.GetFresh(ctx, session.AccessToken,
		fmt.Sprintf("/%d/domains/%s", session.Tenant.AccountID, domainName),
		"/domains"))
	return kit.Chain(status, func(raw domainJSON) kit.Outcome[bool] { return kit.Success(raw.PrivateWhois) })
}

func (gateway RegistrarGateway) SetWhoisPrivacy(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing] {
	method := http.MethodPut
	if !enabled {
		method = http.MethodDelete
	}
	changed := gateway.client.Write(ctx, session.AccessToken, method,
		fmt.Sprintf("/%d/registrar/domains/%s/whois_privacy", session.Tenant.AccountID, domainName),
		"/registrar/whois_privacy", nil)
	return kit.Chain(changed, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway RegistrarGateway) TransferLock(ctx context.Context, session identity.Session, domainName string) kit.Outcome[bool] {
	type lockJSON struct {
		Enabled bool `json:"enabled"`
	}
	status := decodeData[lockJSON](gateway.client.GetFresh(ctx, session.AccessToken,
		fmt.Sprintf("/%d/registrar/domains/%s/transfer_lock", session.Tenant.AccountID, domainName),
		"/registrar/transfer_lock"))
	return disabledWhenAbsent(kit.Chain(status, func(raw lockJSON) kit.Outcome[bool] { return kit.Success(raw.Enabled) }))
}

func (gateway RegistrarGateway) SetTransferLock(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing] {
	method := http.MethodPost
	if !enabled {
		method = http.MethodDelete
	}
	changed := gateway.client.Write(ctx, session.AccessToken, method,
		fmt.Sprintf("/%d/registrar/domains/%s/transfer_lock", session.Tenant.AccountID, domainName),
		"/registrar/transfer_lock", nil)
	return kit.Chain(changed, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway RegistrarGateway) Delegation(ctx context.Context, session identity.Session, domainName string) kit.Outcome[[]string] {
	return decodeData[[]string](gateway.client.GetFresh(ctx, session.AccessToken,
		fmt.Sprintf("/%d/registrar/domains/%s/delegation", session.Tenant.AccountID, domainName),
		"/registrar/delegation"))
}

func (gateway RegistrarGateway) SetDelegation(ctx context.Context, session identity.Session, domainName string, nameServers []string) kit.Outcome[kit.Nothing] {
	changed := gateway.client.Write(ctx, session.AccessToken, http.MethodPut,
		fmt.Sprintf("/%d/registrar/domains/%s/delegation", session.Tenant.AccountID, domainName),
		"/registrar/delegation", nameServers)
	return kit.Chain(changed, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway RegistrarGateway) Renew(ctx context.Context, session identity.Session, domainName string) kit.Outcome[registrar.Renewal] {
	type renewalJSON struct {
		ID    int64  `json:"id"`
		State string `json:"state"`
	}
	renewed := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/registrar/domains/%s/renewals", session.Tenant.AccountID, domainName),
		"/registrar/renewals", map[string]int{"period": 1})
	return kit.Chain(decodeData[renewalJSON](renewed),
		func(raw renewalJSON) kit.Outcome[registrar.Renewal] {
			return kit.Success(registrar.Renewal(raw))
		})
}

type transferJSON struct {
	ID    int64  `json:"id"`
	State string `json:"state"`
}

func (gateway RegistrarGateway) Transfer(ctx context.Context, session identity.Session, domainName string, contactID int64, authCode string) kit.Outcome[registrar.Transfer] {
	accepted := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/registrar/domains/%s/transfers", session.Tenant.AccountID, domainName),
		"/registrar/transfers", map[string]any{"registrant_id": contactID, "auth_code": authCode})
	return kit.Chain(decodeData[transferJSON](accepted),
		func(raw transferJSON) kit.Outcome[registrar.Transfer] {
			return kit.Success(registrar.Transfer(raw))
		})
}

func (gateway RegistrarGateway) TransferState(ctx context.Context, session identity.Session, domainName string, transferID int64) kit.Outcome[registrar.Transfer] {
	polled := decodeData[transferJSON](gateway.client.GetFresh(ctx, session.AccessToken,
		fmt.Sprintf("/%d/registrar/domains/%s/transfers/%d", session.Tenant.AccountID, domainName, transferID),
		"/registrar/transfers"))
	return kit.Chain(polled, func(raw transferJSON) kit.Outcome[registrar.Transfer] {
		return kit.Success(registrar.Transfer(raw))
	})
}

func (gateway RegistrarGateway) AuthorizeTransferOut(ctx context.Context, session identity.Session, domainName string) kit.Outcome[kit.Nothing] {
	authorized := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/registrar/domains/%s/authorize_transfer_out", session.Tenant.AccountID, domainName),
		"/registrar/authorize_transfer_out", nil)
	return kit.Chain(authorized, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway RegistrarGateway) Restore(ctx context.Context, session identity.Session, domainName string) kit.Outcome[registrar.Restore] {
	type restoreJSON struct {
		ID    int64  `json:"id"`
		State string `json:"state"`
	}
	restored := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/registrar/domains/%s/restores", session.Tenant.AccountID, domainName),
		"/registrar/restores", nil)
	return kit.Chain(decodeData[restoreJSON](restored),
		func(raw restoreJSON) kit.Outcome[registrar.Restore] {
			return kit.Success(registrar.Restore(raw))
		})
}

func (gateway RegistrarGateway) RegistrationState(ctx context.Context, session identity.Session, domainName string, registrationID int64) kit.Outcome[registrar.Registration] {
	polled := decodeData[registrationJSON](gateway.client.GetFresh(ctx, session.AccessToken,
		fmt.Sprintf("/%d/registrar/domains/%s/registrations/%d", session.Tenant.AccountID, domainName, registrationID),
		"/registrar/registrations"))
	return kit.Chain(polled, func(raw registrationJSON) kit.Outcome[registrar.Registration] {
		return kit.Success(registrar.Registration(raw))
	})
}

type registrantChangeJSON struct {
	ID        int64  `json:"id"`
	State     string `json:"state"`
	DomainID  int64  `json:"domain_id"`
	ContactID int64  `json:"contact_id"`
}

func toRegistrantChange(raw registrantChangeJSON) registrar.RegistrantChange {
	return registrar.RegistrantChange(raw)
}

func (gateway RegistrarGateway) CheckRegistrantChange(ctx context.Context, session identity.Session, domainID, contactID int64) kit.Outcome[registrar.RegistrantChangeCheck] {
	type checkJSON struct {
		ExtendedAttributes  []map[string]any `json:"extended_attributes"`
		RegistryOwnerChange bool             `json:"registry_owner_change"`
	}
	checked := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/registrar/registrant_changes/check", session.Tenant.AccountID),
		"/registrar/registrant_changes", map[string]int64{"domain_id": domainID, "contact_id": contactID})
	return kit.Chain(decodeData[checkJSON](checked),
		func(raw checkJSON) kit.Outcome[registrar.RegistrantChangeCheck] {
			return kit.Success(registrar.RegistrantChangeCheck{
				RequiresExtendedAttributes: len(raw.ExtendedAttributes) > 0,
				RegistryOwnerChange:        raw.RegistryOwnerChange,
			})
		})
}

func (gateway RegistrarGateway) CreateRegistrantChange(ctx context.Context, session identity.Session, domainID, contactID int64) kit.Outcome[registrar.RegistrantChange] {
	created := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/registrar/registrant_changes", session.Tenant.AccountID),
		"/registrar/registrant_changes", map[string]any{"domain_id": domainID, "contact_id": contactID})
	return kit.Chain(decodeData[registrantChangeJSON](created),
		func(raw registrantChangeJSON) kit.Outcome[registrar.RegistrantChange] {
			return kit.Success(toRegistrantChange(raw))
		})
}

func (gateway RegistrarGateway) RegistrantChangeState(ctx context.Context, session identity.Session, changeID int64) kit.Outcome[registrar.RegistrantChange] {
	polled := decodeData[registrantChangeJSON](gateway.client.GetFresh(ctx, session.AccessToken,
		fmt.Sprintf("/%d/registrar/registrant_changes/%d", session.Tenant.AccountID, changeID),
		"/registrar/registrant_changes"))
	return kit.Chain(polled, func(raw registrantChangeJSON) kit.Outcome[registrar.RegistrantChange] {
		return kit.Success(toRegistrantChange(raw))
	})
}

func (gateway RegistrarGateway) Prices(ctx context.Context, session identity.Session, domainName string) kit.Outcome[float64] {
	type pricesJSON struct {
		RegistrationPrice float64 `json:"registration_price"`
	}
	priced := decodeData[pricesJSON](gateway.client.Get(ctx, session.AccessToken,
		fmt.Sprintf("/%d/registrar/domains/%s/prices", session.Tenant.AccountID, domainName),
		"/registrar/prices"))
	return kit.Chain(priced, func(raw pricesJSON) kit.Outcome[float64] {
		return kit.Success(raw.RegistrationPrice)
	})
}

func (gateway RegistrarGateway) SetVanityNameServers(ctx context.Context, session identity.Session, domainName string, enabled bool) kit.Outcome[kit.Nothing] {
	method := http.MethodPut
	if !enabled {
		method = http.MethodDelete
	}
	changed := gateway.client.Write(ctx, session.AccessToken, method,
		fmt.Sprintf("/%d/vanity/%s", session.Tenant.AccountID, domainName), "/vanity", nil)
	return kit.Chain(changed, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway RegistrarGateway) RegistrantChanges(ctx context.Context, session identity.Session) kit.Outcome[[]registrar.RegistrantChange] {
	listed := listAll[registrantChangeJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/registrar/registrant_changes?per_page=100", session.Tenant.AccountID),
		"/registrar/registrant_changes")
	return kit.Chain(listed, func(raw []registrantChangeJSON) kit.Outcome[[]registrar.RegistrantChange] {
		converted := make([]registrar.RegistrantChange, 0, len(raw))
		for _, change := range raw {
			converted = append(converted, toRegistrantChange(change))
		}
		return kit.Success(converted)
	})
}

func (gateway RegistrarGateway) CancelRegistrantChange(ctx context.Context, session identity.Session, changeID int64) kit.Outcome[kit.Nothing] {
	cancelled := gateway.client.Write(ctx, session.AccessToken, http.MethodDelete,
		fmt.Sprintf("/%d/registrar/registrant_changes/%d", session.Tenant.AccountID, changeID),
		"/registrar/registrant_changes", nil)
	return kit.Chain(cancelled, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway RegistrarGateway) CancelTransfer(ctx context.Context, session identity.Session, domainName string, transferID int64) kit.Outcome[kit.Nothing] {
	cancelled := gateway.client.Write(ctx, session.AccessToken, http.MethodDelete,
		fmt.Sprintf("/%d/registrar/domains/%s/transfers/%d", session.Tenant.AccountID, domainName, transferID),
		"/registrar/transfers", nil)
	return kit.Chain(cancelled, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}
