package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestLogRecordsCarryTheTenantSessionAndBTXFromTheContext(test *testing.T) {
	// Given a logger and a context annotated the way a BTX would see it
	var sink bytes.Buffer
	logger := NewLogger(&sink, slog.LevelInfo)

	annotated := WithTenant(context.Background(), "tenant-2416")
	annotated = WithSession(annotated, "session-abc")
	annotated = WithBTX(annotated, "btx-link-account-7")

	// When a component logs through that context
	logger.InfoContext(annotated, "token vaulted")

	// Then the record carries all three identifiers without the call site naming them
	var record map[string]any
	if err := json.Unmarshal(sink.Bytes(), &record); err != nil {
		test.Fatalf("log output is not JSON: %v (%s)", err, sink.String())
	}
	for field, expected := range map[string]string{
		"tenant_id":  "tenant-2416",
		"session_id": "session-abc",
		"btx_id":     "btx-link-account-7",
		"msg":        "token vaulted",
	} {
		if record[field] != expected {
			test.Fatalf("field %q: expected %q, got %v in %s", field, expected, record[field], sink.String())
		}
	}
}

func TestLogRecordsOmitIdentifiersTheContextDoesNotHave(test *testing.T) {
	// Given a logger and a bare context
	var sink bytes.Buffer
	logger := NewLogger(&sink, slog.LevelInfo)

	// When a component logs without annotations
	logger.InfoContext(context.Background(), "booting")

	// Then no empty identifier fields pollute the record
	var record map[string]any
	if err := json.Unmarshal(sink.Bytes(), &record); err != nil {
		test.Fatalf("log output is not JSON: %v", err)
	}
	for _, field := range []string{"tenant_id", "session_id", "btx_id"} {
		if _, present := record[field]; present {
			test.Fatalf("field %q should be absent on a bare context: %s", field, sink.String())
		}
	}
}
