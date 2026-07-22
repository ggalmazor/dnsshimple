package dnsimple

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"dnsshimple/core/identity"
	"dnsshimple/core/zones"
	"dnsshimple/kit"
)

// ZonesGateway implements the zones port on the rate-limited Client.
type ZonesGateway struct {
	client *Client
}

func NewZonesGateway(client *Client) ZonesGateway {
	return ZonesGateway{client: client}
}

var _ zones.Gateway = ZonesGateway{}

type recordJSON struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Priority *int   `json:"priority"`
}

func (gateway ZonesGateway) Zones(ctx context.Context, session identity.Session) kit.Outcome[[]zones.Zone] {
	return listAll[zones.Zone](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/zones?per_page=100", session.Tenant.AccountID), "/zones")
}

func (gateway ZonesGateway) Records(ctx context.Context, session identity.Session, zoneName string) kit.Outcome[[]zones.Record] {
	raw := listAll[recordJSON](gateway.client, ctx, session.AccessToken,
		fmt.Sprintf("/%d/zones/%s/records?per_page=100", session.Tenant.AccountID, zoneName), "/zones/records")
	return kit.Chain(raw, func(records []recordJSON) kit.Outcome[[]zones.Record] {
		converted := make([]zones.Record, 0, len(records))
		for _, record := range records {
			converted = append(converted, toRecord(record))
		}
		return kit.Success(converted)
	})
}

func (gateway ZonesGateway) CreateRecord(ctx context.Context, session identity.Session, zoneName string, draft zones.RecordDraft) kit.Outcome[zones.Record] {
	created := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/zones/%s/records", session.Tenant.AccountID, zoneName),
		"/zones/records", draftPayload(draft))
	return kit.Chain(decodeData[recordJSON](created),
		func(record recordJSON) kit.Outcome[zones.Record] { return kit.Success(toRecord(record)) })
}

func (gateway ZonesGateway) UpdateRecord(ctx context.Context, session identity.Session, zoneName string, recordID int64, draft zones.RecordDraft) kit.Outcome[zones.Record] {
	updated := gateway.client.Write(ctx, session.AccessToken, http.MethodPatch,
		fmt.Sprintf("/%d/zones/%s/records/%d", session.Tenant.AccountID, zoneName, recordID),
		"/zones/records", draftPayload(draft))
	return kit.Chain(decodeData[recordJSON](updated),
		func(record recordJSON) kit.Outcome[zones.Record] { return kit.Success(toRecord(record)) })
}

func (gateway ZonesGateway) DeleteRecord(ctx context.Context, session identity.Session, zoneName string, recordID int64) kit.Outcome[kit.Nothing] {
	deleted := gateway.client.Write(ctx, session.AccessToken, http.MethodDelete,
		fmt.Sprintf("/%d/zones/%s/records/%d", session.Tenant.AccountID, zoneName, recordID),
		"/zones/records", nil)
	return kit.Chain(deleted, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

func (gateway ZonesGateway) SubmitBatch(ctx context.Context, session identity.Session, zoneName string, change zones.BatchChange) kit.Outcome[kit.Nothing] {
	deletes := make([]map[string]int64, 0, len(change.Deletes))
	for _, recordID := range change.Deletes {
		deletes = append(deletes, map[string]int64{"id": recordID})
	}
	updates := make([]map[string]any, 0, len(change.Updates))
	for _, update := range change.Updates {
		payload := draftPayload(zones.RecordDraft{
			Name: update.Name, Type: update.Type, Content: update.Content,
			TTL: update.TTL, Priority: update.Priority,
		})
		payload["id"] = update.ID
		updates = append(updates, payload)
	}
	creates := make([]map[string]any, 0, len(change.Creates))
	for _, draft := range change.Creates {
		creates = append(creates, draftPayload(draft))
	}

	submitted := gateway.client.Write(ctx, session.AccessToken, http.MethodPost,
		fmt.Sprintf("/%d/zones/%s/batch", session.Tenant.AccountID, zoneName),
		"/zones/batch", map[string]any{"creates": creates, "updates": updates, "deletes": deletes})
	return kit.Chain(submitted, func([]byte) kit.Outcome[kit.Nothing] { return kit.Done() })
}

// --- payload plumbing -----------------------------------------------------

func draftPayload(draft zones.RecordDraft) map[string]any {
	payload := map[string]any{
		"name":    draft.Name,
		"type":    draft.Type,
		"content": draft.Content,
		"ttl":     draft.TTL,
	}
	if draft.Priority > 0 {
		payload["priority"] = draft.Priority
	}
	return payload
}

func toRecord(record recordJSON) zones.Record {
	converted := zones.Record{
		ID: record.ID, Name: record.Name, Type: record.Type,
		Content: record.Content, TTL: record.TTL,
	}
	if record.Priority != nil {
		converted.Priority = *record.Priority
	}
	return converted
}

// decodeData unwraps DNSimple's {"data": ...} envelope on the railway.
func decodeData[Payload any](outcome kit.Outcome[[]byte]) kit.Outcome[Payload] {
	return kit.Chain(outcome, func(body []byte) kit.Outcome[Payload] {
		var envelope struct {
			Data Payload `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return kit.Failure[Payload](fmt.Errorf("decoding the DNSimple response: %w", err))
		}
		return kit.Success(envelope.Data)
	})
}
