// Package telemetry is the infrastructure Component every other Component
// reports through: structured logs, usage metrics, and (later) traces.
// See DESIGN.md §7.
package telemetry

import (
	"context"
	"io"
	"log/slog"
)

type contextKey string

const (
	tenantKey  contextKey = "tenant_id"
	sessionKey contextKey = "session_id"
	btxKey     contextKey = "btx_id"
)

// WithTenant annotates the context so every later log record names the tenant.
func WithTenant(parent context.Context, tenantID string) context.Context {
	return context.WithValue(parent, tenantKey, tenantID)
}

// WithSession annotates the context with the SSH session identity.
func WithSession(parent context.Context, sessionID string) context.Context {
	return context.WithValue(parent, sessionKey, sessionID)
}

// WithBTX annotates the context with the running Business Transaction.
func WithBTX(parent context.Context, btxID string) context.Context {
	return context.WithValue(parent, btxKey, btxID)
}

// contextHandler lifts the tenant, session, and BTX identifiers out of the
// context into every record, so no call site repeats them and no
// troubleshooting session starts with an unattributable line.
type contextHandler struct {
	inner slog.Handler
}

func (handler contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.inner.Enabled(ctx, level)
}

func (handler contextHandler) Handle(ctx context.Context, record slog.Record) error {
	for _, carrier := range []contextKey{tenantKey, sessionKey, btxKey} {
		if value, present := ctx.Value(carrier).(string); present {
			record.AddAttrs(slog.String(string(carrier), value))
		}
	}
	return handler.inner.Handle(ctx, record)
}

func (handler contextHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	return contextHandler{inner: handler.inner.WithAttrs(attributes)}
}

func (handler contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{inner: handler.inner.WithGroup(name)}
}

// NewLogger builds the JSON logger every process uses: machine-parseable
// records that automatically carry the identity annotations of their context.
func NewLogger(sink io.Writer, level slog.Leveler) *slog.Logger {
	return slog.New(contextHandler{
		inner: slog.NewJSONHandler(sink, &slog.HandlerOptions{Level: level}),
	})
}
