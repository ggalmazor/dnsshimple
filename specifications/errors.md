# Errors

## Families

Every error crossing a facade belongs to one family, which decides what
the caller does:

- **Temporary** - retry or reschedule. `gateway.RateLimitedError`
  (carries `ResumeAt`), `gateway.ErrUpstreamDown`,
  `gateway.UpstreamTimeoutError` (says the write "may still have
  completed").
- **Permanent** - a known failure mode the caller handles or shows.
  Typed domain errors: `registrar.InvalidDomainError`,
  `domains.InvalidDomainNameError`, `zones.InvalidRecordError`,
  `registrar.NeedsExtendedAttributesError`,
  `gateway.UpstreamRejectionError` (carries status and DNSimple's own
  message).
- **Unexpected** - everything else bubbles up and fails the operation.

## Rules

1. Validation errors are typed structs with a `Reason`, raised before any
   API spend, so forms can reopen with the message.
2. Refusals that require caller action carry their payload (see
   business-transactions.md rule 5).
3. The TUI translates errors to humans in exactly one place (`statusFor`);
   messages are actionable ("DNSimple says: ...", "may still have
   completed - refresh before retrying"), never raw error chains.
4. 404-means-disabled translations are only allowed where production
   semantics say absence is a state (transfer lock); never to paper over
   calling a route that does not exist.
