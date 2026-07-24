package dnsimple

import (
	"context"
	"fmt"
	"net/http"

	"dnsshimple/core/identity"
	"dnsshimple/core/templates"
	"dnsshimple/core/zones"
	"dnsshimple/kit"
)

type TemplatesGateway struct {
	client *Client
}

func NewTemplatesGateway(client *Client) TemplatesGateway {
	return TemplatesGateway{client: client}
}

var _ templates.Gateway = TemplatesGateway{}

type templateJSON struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	SID         string `json:"sid"`
	Description string `json:"description"`
}

func (gateway TemplatesGateway) Templates(ctx context.Context, session identity.Session) kit.Outcome[[]templates.Template] {
	listed := listAll[templateJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/templates?per_page=100", session.Tenant.AccountID), "/templates")
	return kit.Chain(listed, func(raw []templateJSON) kit.Outcome[[]templates.Template] {
		converted := make([]templates.Template, 0, len(raw))
		for _, template := range raw {
			converted = append(converted, templates.Template(template))
		}
		return kit.Success(converted)
	})
}

func (gateway TemplatesGateway) CreateTemplate(ctx context.Context, session identity.Session, template templates.Template) kit.Outcome[templates.Template] {
	created := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/templates", session.Tenant.AccountID), "/templates",
		map[string]string{"name": template.Name, "sid": template.SID, "description": template.Description})
	return kit.Chain(decodeData[templateJSON](created),
		func(raw templateJSON) kit.Outcome[templates.Template] {
			return kit.Success(templates.Template(raw))
		})
}

func (gateway TemplatesGateway) DeleteTemplate(ctx context.Context, session identity.Session, templateID int64) kit.Outcome[kit.Nothing] {
	deleted := gateway.client.Write(ctx, session.AccessToken, http.MethodDelete,
		fmt.Sprintf("/%d/templates/%d", session.Tenant.AccountID, templateID), "/templates", nil)
	return kit.Chain(deleted, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway TemplatesGateway) TemplateRecords(ctx context.Context, session identity.Session, templateID int64) kit.Outcome[[]zones.Record] {
	listed := listAll[recordJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/templates/%d/records?per_page=100", session.Tenant.AccountID, templateID),
		"/templates/records")
	return kit.Chain(listed, func(raw []recordJSON) kit.Outcome[[]zones.Record] {
		converted := make([]zones.Record, 0, len(raw))
		for _, record := range raw {
			converted = append(converted, toRecord(record))
		}
		return kit.Success(converted)
	})
}

func (gateway TemplatesGateway) CreateTemplateRecord(ctx context.Context, session identity.Session, templateID int64, draft zones.RecordDraft) kit.Outcome[zones.Record] {
	created := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/templates/%d/records", session.Tenant.AccountID, templateID),
		"/templates/records", draftPayload(draft))
	return kit.Chain(decodeData[recordJSON](created),
		func(record recordJSON) kit.Outcome[zones.Record] { return kit.Success(toRecord(record)) })
}

func (gateway TemplatesGateway) DeleteTemplateRecord(ctx context.Context, session identity.Session, templateID, recordID int64) kit.Outcome[kit.Nothing] {
	deleted := gateway.client.Write(ctx, session.AccessToken, http.MethodDelete,
		fmt.Sprintf("/%d/templates/%d/records/%d", session.Tenant.AccountID, templateID, recordID),
		"/templates/records", nil)
	return kit.Chain(deleted, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway TemplatesGateway) ApplyTemplate(ctx context.Context, session identity.Session, domainName string, templateID int64) kit.Outcome[kit.Nothing] {
	applied := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/domains/%s/templates/%d", session.Tenant.AccountID, domainName, templateID),
		"/domains/templates", nil)
	return kit.Chain(applied, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway TemplatesGateway) UpdateTemplate(ctx context.Context, session identity.Session, template templates.Template) kit.Outcome[templates.Template] {
	updated := gateway.client.Write(ctx, session.AccessToken, http.MethodPatch,
		fmt.Sprintf("/%d/templates/%d", session.Tenant.AccountID, template.ID), "/templates",
		map[string]string{"name": template.Name, "sid": template.SID, "description": template.Description})
	return kit.Chain(decodeData[templateJSON](updated),
		func(raw templateJSON) kit.Outcome[templates.Template] {
			return kit.Success(templates.Template(raw))
		})
}
