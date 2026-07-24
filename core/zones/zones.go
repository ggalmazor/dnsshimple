// Package zones is the Component that manages DNS zones and their records
// (DESIGN.md §3.1): listing, record CRUD, and the BatchEditRecords Business
// Transaction.
package zones

import (
	"context"
	"fmt"

	"dnsshimple/core/identity"
	"dnsshimple/kit"
)

type Zone struct {
	ID     int64
	Name   string
	Active bool
}

type Record struct {
	ID       int64
	Name     string
	Type     string
	Content  string
	TTL      int
	Priority int
}

type RecordDraft struct {
	Name     string
	Type     string
	Content  string
	TTL      int
	Priority int
}

// BatchChange is the payload of the BatchEditRecords BTX, mapping to
// DNSimple's POST /{account}/zones/{zone}/batch.
type BatchChange struct {
	Creates []RecordDraft
	Updates []Record
	Deletes []int64
}

// InvalidRecordError says the draft never left the process: it names exactly
// what is wrong so the form can show it next to the field.
type InvalidRecordError struct {
	Reason string
}

func (invalid InvalidRecordError) Error() string {
	return "zones: invalid record: " + invalid.Reason
}

type Gateway interface {
	Zones(ctx context.Context, session identity.Session) kit.Outcome[[]Zone]
	Records(ctx context.Context, session identity.Session, zoneName string) kit.Outcome[[]Record]
	CreateRecord(ctx context.Context, session identity.Session, zoneName string, draft RecordDraft) kit.Outcome[Record]
	UpdateRecord(ctx context.Context, session identity.Session, zoneName string, recordID int64, draft RecordDraft) kit.Outcome[Record]
	DeleteRecord(ctx context.Context, session identity.Session, zoneName string, recordID int64) kit.Outcome[kit.Nothing]
	SubmitBatch(ctx context.Context, session identity.Session, zoneName string, change BatchChange) kit.Outcome[kit.Nothing]
	SetZoneActivation(ctx context.Context, session identity.Session, zoneName string, active bool) kit.Outcome[kit.Nothing]
	ZoneDistribution(ctx context.Context, session identity.Session, zoneName string) kit.Outcome[bool]
	ZoneFile(ctx context.Context, session identity.Session, zoneName string) kit.Outcome[string]
	RecordDistribution(ctx context.Context, session identity.Session, zoneName string, recordID int64) kit.Outcome[bool]
}

// recordTypes DNSSHimple lets users write. Kept deliberately to the common
// set; exotic types can be added as the TUI grows forms for them.
var recordTypes = map[string]bool{
	"A": true, "AAAA": true, "ALIAS": true, "CAA": true, "CNAME": true,
	"MX": true, "NS": true, "SRV": true, "TXT": true, "URL": true,
}

const (
	minimumTTL = 60     // DNSimple's floor
	maximumTTL = 604800 // 7 days; longer is almost always a mistake in a TUI
)

// ValidateDraft rejects drafts that DNSimple would reject anyway, before a
// request is spent on them.
func ValidateDraft(draft RecordDraft) error {
	if !recordTypes[draft.Type] {
		return InvalidRecordError{Reason: fmt.Sprintf("type %q is not supported", draft.Type)}
	}
	if draft.Content == "" {
		return InvalidRecordError{Reason: "content must not be empty"}
	}
	if draft.TTL < minimumTTL || draft.TTL > maximumTTL {
		return InvalidRecordError{Reason: fmt.Sprintf("TTL must be between %d and %d", minimumTTL, maximumTTL)}
	}
	if (draft.Type == "MX" || draft.Type == "SRV") && draft.Priority <= 0 {
		return InvalidRecordError{Reason: draft.Type + " records need a positive priority"}
	}
	return nil
}
