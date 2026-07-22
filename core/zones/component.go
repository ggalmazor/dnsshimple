package zones

import (
	"context"
	"log/slog"

	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
	"dnsshimple/kit"
)

// Component is the zones facade the TUI talks to: validation in front,
// the rate-limited gateway behind.
type Component struct {
	gateway Gateway
	metrics *telemetry.Metrics
}

func NewComponent(gateway Gateway, metrics *telemetry.Metrics) *Component {
	return &Component{gateway: gateway, metrics: metrics}
}

func (component *Component) Zones(ctx context.Context, session identity.Session) kit.Outcome[[]Zone] {
	return component.gateway.Zones(ctx, session)
}

func (component *Component) Records(ctx context.Context, session identity.Session, zoneName string) kit.Outcome[[]Record] {
	return component.gateway.Records(ctx, session, zoneName)
}

func (component *Component) CreateRecord(ctx context.Context, session identity.Session, zoneName string, draft RecordDraft) kit.Outcome[Record] {
	if err := ValidateDraft(draft); err != nil {
		return kit.Failure[Record](err)
	}
	return component.gateway.CreateRecord(ctx, session, zoneName, draft)
}

func (component *Component) UpdateRecord(ctx context.Context, session identity.Session, zoneName string, recordID int64, draft RecordDraft) kit.Outcome[Record] {
	if err := ValidateDraft(draft); err != nil {
		return kit.Failure[Record](err)
	}
	return component.gateway.UpdateRecord(ctx, session, zoneName, recordID, draft)
}

func (component *Component) DeleteRecord(ctx context.Context, session identity.Session, zoneName string, recordID int64) kit.Outcome[kit.Nothing] {
	return component.gateway.DeleteRecord(ctx, session, zoneName, recordID)
}

// BatchEditRecords is the sync BTX from DESIGN.md §3.2: validate the whole
// change set first, then submit it as one API call, so a half-valid set
// never partially lands.
func (component *Component) BatchEditRecords(ctx context.Context, session identity.Session, zoneName string, change BatchChange) kit.Outcome[kit.Nothing] {
	ctx = telemetry.WithBTX(ctx, "BatchEditRecords:"+zoneName)

	for _, draft := range change.Creates {
		if err := ValidateDraft(draft); err != nil {
			component.metrics.BTXOutcomes.WithLabelValues("BatchEditRecords", "rejected").Inc()
			return kit.Failure[kit.Nothing](err)
		}
	}
	for _, update := range change.Updates {
		if err := ValidateDraft(RecordDraft{
			Name: update.Name, Type: update.Type, Content: update.Content,
			TTL: update.TTL, Priority: update.Priority,
		}); err != nil {
			component.metrics.BTXOutcomes.WithLabelValues("BatchEditRecords", "rejected").Inc()
			return kit.Failure[kit.Nothing](err)
		}
	}

	submitted := component.gateway.SubmitBatch(ctx, session, zoneName, change)
	if submitted.Failed() {
		component.metrics.BTXOutcomes.WithLabelValues("BatchEditRecords", "failed").Inc()
		slog.ErrorContext(ctx, "BatchEditRecords failed", "cause", submitted.Error())
		return submitted
	}
	component.metrics.BTXOutcomes.WithLabelValues("BatchEditRecords", "completed").Inc()
	slog.InfoContext(ctx, "BatchEditRecords completed",
		"creates", len(change.Creates), "updates", len(change.Updates), "deletes", len(change.Deletes))
	return submitted
}
