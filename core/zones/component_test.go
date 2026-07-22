package zones

import (
	"context"
	"errors"
	"testing"

	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
	"dnsshimple/kit"
)

func TestInvalidDraftsNeverReachTheGateway(test *testing.T) {
	// Given the Component over a gateway that must not be touched
	gateway := &recordingGateway{}
	component := NewComponent(gateway, telemetry.NewMetrics())
	session := identity.Session{}
	ctx := context.Background()

	// When clearly invalid drafts are submitted through every mutating door
	invalidDrafts := map[string]RecordDraft{
		"unsupported type": {Name: "www", Type: "WKS", Content: "x", TTL: 3600},
		"empty content":    {Name: "www", Type: "A", Content: "", TTL: 3600},
		"TTL below floor":  {Name: "www", Type: "A", Content: "1.2.3.4", TTL: 5},
		"MX no priority":   {Name: "", Type: "MX", Content: "mail.example.com", TTL: 3600},
	}
	for name, draft := range invalidDrafts {
		created := component.CreateRecord(ctx, session, "example.com", draft)
		batched := component.BatchEditRecords(ctx, session, "example.com", BatchChange{Creates: []RecordDraft{draft}})

		// Then both fail with the typed validation error
		var invalid InvalidRecordError
		if !created.Failed() || !errors.As(created.Error(), &invalid) {
			test.Fatalf("%s: create must fail typed, got %v", name, created.Error())
		}
		if !batched.Failed() || !errors.As(batched.Error(), &invalid) {
			test.Fatalf("%s: batch must fail typed, got %v", name, batched.Error())
		}
	}

	// And the gateway never saw a single call
	if gateway.calls != 0 {
		test.Fatalf("the gateway was called %d times for invalid drafts", gateway.calls)
	}
}

func TestValidDraftsPassThroughToTheGateway(test *testing.T) {
	gateway := &recordingGateway{}
	component := NewComponent(gateway, telemetry.NewMetrics())

	outcome := component.CreateRecord(context.Background(), identity.Session{}, "example.com",
		RecordDraft{Name: "mail", Type: "MX", Content: "mail.example.com", TTL: 3600, Priority: 10})

	if outcome.Failed() || gateway.calls != 1 {
		test.Fatalf("a valid draft must reach the gateway exactly once: %v (calls %d)",
			outcome.Error(), gateway.calls)
	}
}

// recordingGateway counts calls and succeeds blandly.
type recordingGateway struct {
	calls int
}

func (gateway *recordingGateway) Zones(context.Context, identity.Session) kit.Outcome[[]Zone] {
	gateway.calls++
	return kit.Success([]Zone{})
}

func (gateway *recordingGateway) Records(context.Context, identity.Session, string) kit.Outcome[[]Record] {
	gateway.calls++
	return kit.Success([]Record{})
}

func (gateway *recordingGateway) CreateRecord(_ context.Context, _ identity.Session, _ string, draft RecordDraft) kit.Outcome[Record] {
	gateway.calls++
	return kit.Success(Record{ID: 1, Name: draft.Name, Type: draft.Type, Content: draft.Content, TTL: draft.TTL})
}

func (gateway *recordingGateway) UpdateRecord(_ context.Context, _ identity.Session, _ string, recordID int64, draft RecordDraft) kit.Outcome[Record] {
	gateway.calls++
	return kit.Success(Record{ID: recordID, Name: draft.Name, Type: draft.Type, Content: draft.Content, TTL: draft.TTL})
}

func (gateway *recordingGateway) DeleteRecord(context.Context, identity.Session, string, int64) kit.Outcome[kit.Nothing] {
	gateway.calls++
	return kit.Done()
}

func (gateway *recordingGateway) SubmitBatch(context.Context, identity.Session, string, BatchChange) kit.Outcome[kit.Nothing] {
	gateway.calls++
	return kit.Done()
}
