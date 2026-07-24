# Business Transactions (BTX)

## Definition

A BTX is a named unit of business work orchestrating one or more
components: the entrypoint for anything a user action triggers. Sync BTX
run in the session goroutine and return their outcome; async BTX accept
the work, enqueue a workflow (see [workflows.md](workflows.md)), and
return the accepted state.

## Rules

1. A BTX lives as a method on the component that owns the operation's
   primary domain (RegisterDomain on registrar, InspectDomain on domains).
2. Pure orchestration: domain rules live behind facades; the BTX sequences
   facade calls and routes outcomes. Deleting a BTX must lose only
   sequencing, never a rule.
3. Every BTX opens with `ctx = telemetry.WithBTX(ctx, "<Name>:<subject>")`
   and counts terminal outcomes in `metrics.BTXOutcomes` with labels
   `accepted`, `completed`, or `failed`.
4. No business logic in driving adapters - the Bubble Tea models render
   state and dispatch BTX, nothing else. Cross-component sequencing found
   in a screen is an un-extracted BTX: name it and move it.
5. Refusals are typed: when a BTX cannot proceed for a reason the caller
   must act on, it fails with a typed error carrying what the caller needs
   (`NeedsExtendedAttributesError.Demands` carries the form definition).
   A driving adapter renders the payload; it never re-derives it.
6. Read aggregations that span components (InspectDomain) are sync BTX:
   fan out concurrently, degrade per field, and report partial failures in
   the result rather than failing the whole read.
