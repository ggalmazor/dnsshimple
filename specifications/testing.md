# Testing

## Style

Grey-box TDD is the default: drive a component through its public port
against real infrastructure (Postgres) and a stateful in-process DNSimple
fake, assert only on observable outcomes. Test names are sentences:
`TestARegistrationAdvancesToRegisteredAndLandsInTheDomainList`.

## Rules

1. Postgres tests run in the dedicated `dnsshimple_tests` schema (via
   search_path, guarded by a `current_schema()` check). Never against the
   dev database's default schema.
2. The fake drifts toward production, never toward our assumptions: every
   grey-box exchange in the dnsimple adapter package runs through
   `newContractServer`, validating against the vendored OpenAPI spec
   (`testdata/openapi.yml`, `required` stripped - the fake may say less
   than production, never something production could not say). Exemptions
   are documented at the exemption site and only where the spec
   contradicts observed production behavior.
3. Core policy (validation, state machines, `DecidePollStep`) gets pure
   unit tests; no fake, no database.
4. TUI layout is pinned by golden captures (`go test -update` rewrites)
   and invariant tests (every filterable screen advertises `/ filter`).
   New list screens must extend both.
5. The full suite runs `-race`; a slice ships only when all packages pass
   and `golangci-lint` is clean.
6. Bulk edits to generated-looking tables (screen actions, fake routes)
   assert their anchors - a silent no-op replace has bitten twice.
