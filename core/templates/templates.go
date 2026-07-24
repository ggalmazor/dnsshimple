// Package templates is the Component for record templates (DESIGN.md §3.1,
// M7): reusable record sets and the ApplyTemplate Business Transaction.
// Template records share the zones vocabulary (drafts and validation), so a
// record valid in a zone is valid in a template and vice versa.
package templates

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"dnsshimple/core/identity"
	"dnsshimple/core/telemetry"
	"dnsshimple/core/zones"
	"dnsshimple/kit"
)

type Template struct {
	ID          int64
	Name        string
	SID         string
	Description string
}

type InvalidTemplateError struct {
	Reason string
}

func (invalid InvalidTemplateError) Error() string {
	return "templates: invalid template: " + invalid.Reason
}

var sidPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

type Gateway interface {
	Templates(ctx context.Context, session identity.Session) kit.Outcome[[]Template]
	CreateTemplate(ctx context.Context, session identity.Session, template Template) kit.Outcome[Template]
	DeleteTemplate(ctx context.Context, session identity.Session, templateID int64) kit.Outcome[kit.Nothing]
	UpdateTemplate(ctx context.Context, session identity.Session, template Template) kit.Outcome[Template]
	TemplateRecords(ctx context.Context, session identity.Session, templateID int64) kit.Outcome[[]zones.Record]
	CreateTemplateRecord(ctx context.Context, session identity.Session, templateID int64, draft zones.RecordDraft) kit.Outcome[zones.Record]
	DeleteTemplateRecord(ctx context.Context, session identity.Session, templateID, recordID int64) kit.Outcome[kit.Nothing]
	ApplyTemplate(ctx context.Context, session identity.Session, domainName string, templateID int64) kit.Outcome[kit.Nothing]
}

type Component struct {
	gateway Gateway
	metrics *telemetry.Metrics
}

func NewComponent(gateway Gateway, metrics *telemetry.Metrics) *Component {
	return &Component{gateway: gateway, metrics: metrics}
}

func (component *Component) Templates(ctx context.Context, session identity.Session) kit.Outcome[[]Template] {
	return component.gateway.Templates(ctx, session)
}

func (component *Component) CreateTemplate(ctx context.Context, session identity.Session, template Template) kit.Outcome[Template] {
	template.Name = strings.TrimSpace(template.Name)
	template.SID = strings.ToLower(strings.TrimSpace(template.SID))
	if template.Name == "" {
		return kit.Failure[Template](InvalidTemplateError{Reason: "the name must not be empty"})
	}
	if !sidPattern.MatchString(template.SID) {
		return kit.Failure[Template](InvalidTemplateError{Reason: "the short name must be lowercase letters, digits, and hyphens"})
	}
	return component.gateway.CreateTemplate(ctx, session, template)
}

func (component *Component) DeleteTemplate(ctx context.Context, session identity.Session, templateID int64) kit.Outcome[kit.Nothing] {
	return component.gateway.DeleteTemplate(ctx, session, templateID)
}

func (component *Component) TemplateRecords(ctx context.Context, session identity.Session, templateID int64) kit.Outcome[[]zones.Record] {
	return component.gateway.TemplateRecords(ctx, session, templateID)
}

func (component *Component) CreateTemplateRecord(ctx context.Context, session identity.Session, templateID int64, draft zones.RecordDraft) kit.Outcome[zones.Record] {
	if err := zones.ValidateDraft(draft); err != nil {
		return kit.Failure[zones.Record](err)
	}
	return component.gateway.CreateTemplateRecord(ctx, session, templateID, draft)
}

func (component *Component) DeleteTemplateRecord(ctx context.Context, session identity.Session, templateID, recordID int64) kit.Outcome[kit.Nothing] {
	return component.gateway.DeleteTemplateRecord(ctx, session, templateID, recordID)
}

// ApplyTemplate is a sync BTX: it lands every template record in the
// domain's zone as one operation, so it counts and logs like one.
func (component *Component) ApplyTemplate(ctx context.Context, session identity.Session, domainName string, templateID int64) kit.Outcome[kit.Nothing] {
	ctx = telemetry.WithBTX(ctx, "ApplyTemplate:"+domainName)
	applied := component.gateway.ApplyTemplate(ctx, session, domainName, templateID)
	if applied.Failed() {
		component.metrics.BTXOutcomes.WithLabelValues("ApplyTemplate", "failed").Inc()
		slog.ErrorContext(ctx, "ApplyTemplate failed", "cause", applied.Error())
		return applied
	}
	component.metrics.BTXOutcomes.WithLabelValues("ApplyTemplate", "completed").Inc()
	slog.InfoContext(ctx, "ApplyTemplate completed", "template_id", templateID)
	return applied
}

func (component *Component) UpdateTemplate(ctx context.Context, session identity.Session, template Template) kit.Outcome[Template] {
	template.Name = strings.TrimSpace(template.Name)
	template.SID = strings.ToLower(strings.TrimSpace(template.SID))
	if template.Name == "" {
		return kit.Failure[Template](InvalidTemplateError{Reason: "the name must not be empty"})
	}
	if !sidPattern.MatchString(template.SID) {
		return kit.Failure[Template](InvalidTemplateError{Reason: "the short name must be lowercase letters, digits, and hyphens"})
	}
	return component.gateway.UpdateTemplate(ctx, session, template)
}
