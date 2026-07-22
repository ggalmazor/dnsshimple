# Changelog

All notable changes to DNSShimple are documented in this file. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Stack refresh (2026-07-22): every Go module bumped to its latest version
  (notably `charmbracelet/log` v1.0.0); Go stays on 1.26.5 (newest release).
  Full suite green, govulncheck clean except the accepted openpgp advisory.

### Fixed

- The Postgres grey-box suite no longer wipes the developer's real data: it
  pointed at the same database the dev server uses and truncated `tenants`
  (and with it every linked SSH key) on each run, forcing a fresh OAuth link
  after every test run. Tests now use an auto-created `dnsshimple_test`
  database, refuse to run against any name not ending in `_test`, and the
  compose Postgres gained a named volume so dev data survives container
  recreation. One-time cost: relink once after this lands.
- Registration POSTs no longer die at 15 s while DNSimple completes them
  server-side: deadlines are now per-call in the gateway client (15 s reads,
  90 s registration writes) instead of one blanket `http.Client` timeout.
- Error messages are actionable: 4xx failures carry DNSimple's own
  `message` (typed `UpstreamRejectionError`, shown as "DNSimple says: ..."),
  timeouts admit the operation may still have completed and say to refresh
  before retrying (typed `UpstreamTimeoutError`), and every typed registrar
  failure has a status line naming the next action.

## [0.4.0] - 2026-07-22

M3: the domain portfolio and the first async workflow. Browse domains,
register one from the terminal, and let River poll DNSimple to completion in
the background. Plus the first dependency audit and its guardrails.

### Added

- `core/domains/` and `core/registrar/`: the domain portfolio read path and
  the `RegisterDomain` async BTX - validate, check availability, register
  with the account's first contact, and hand state polling to the job queue.
  Typed failures: `ErrDomainTaken`, `ErrNoContacts`, `InvalidDomainError`.
- `adapters/jobs/`: River on the existing Postgres runs the registration
  poll. Rate limits snooze the job to DNSimple's own reset (minimum a
  minute), an open circuit backs off, other failures use River's retries;
  the River schema migrates on boot next to the app schema.
- `adapters/dnsimple/Client.GetFresh`: uncached reads for workflow polls -
  the 45 s cache would have fed every poll tick the same in-flight state.
- TUI: a home menu (Zones / Domains), the domains screen (state, renewal
  mode, expiry), and the register-a-domain form.
- The fake grew domains, contacts, and a registration state machine that
  reaches `registered` on the second poll.

### Security

- Dependency audit (govulncheck, 2026-07-22): Go pinned to 1.26.5, clearing
  GO-2026-5856 (crypto/tls ECH privacy leak, reachable from our listeners and
  client) and GO-2026-4970 (os root escape, in imported packages). The one
  remaining advisory, GO-2026-5932, flags `golang.org/x/crypto/openpgp` as
  unmaintained; we never import it (we use `nacl/secretbox` and `ssh` from
  that module), no fix exists, and govulncheck confirms it is not called.
- CI now runs govulncheck on every push; Dependabot watches Go modules and
  GitHub Actions weekly, with routine bumps grouped and security updates
  arriving individually.

## [0.3.0] - 2026-07-22

M2: zones management behind the rate-limited gateway. Browse zones, edit
records from the terminal, and every DNSimple call now rides the protection
stack (budget, cache, singleflight, breaker).

### Added

- `adapters/dnsimple/Client`: the rate-limit-aware door to the DNSimple API
  (DESIGN.md §5). Per-token budget seeded from `X-RateLimit-*` headers with
  preemptive refusal when exhausted, typed `gateway.RateLimitedError` carrying
  DNSimple's own reset time, a 45 s per-tenant read cache invalidated by any
  write, single-flighting of identical concurrent reads, and a circuit
  breaker (3 consecutive 5xx opens it for 30 s, typed
  `gateway.ErrUpstreamDown`). New metrics: remaining-budget gauge and cache
  hit/miss counters. Endpoint metric labels are caller-provided constants, so
  account ids and zone names never become label cardinality.
- `core/gateway/`: the gateway failure vocabulary Components and screens
  branch on.
- `core/zones/`: the zones Component. Zone and record listing, record CRUD
  with client-side validation (supported types, TTL floor/ceiling, MX/SRV
  priority) that rejects bad drafts before an API request is spent, typed
  `InvalidRecordError` for the form, and the `BatchEditRecords` sync BTX
  (validate the whole set, submit as one call).
- `adapters/dnsimple/ZonesGateway`: the zones port on the rate-limited
  Client, mapping to `/{account}/zones/**` including the batch endpoint.
  Single page of 100 for now; pagination when an account outgrows it.
- The zones browser in the TUI: zone list -> record list -> record form
  (create/edit via `bubbles/textinput`) -> delete confirmation. Validation
  failures keep the form open with the reason; rate limits show as a status
  line with DNSimple's own resume time; the circuit breaker shows as a
  backing-off notice.
- The DNSimple fake is now stateful: zones API with records, rate-limit
  headers on every response, and `Force429`/`Force500`/`APICalls` helpers for
  exercising the protection stack.

## [0.2.0] - 2026-07-22

M1: the identity Component end to end. `ssh` in, link a DNSimple account once
through the browser handshake, and reconnect by key alone - now on the real
architecture (hexagonal Components, Postgres, sealed tokens, telemetry)
instead of the spike.

### Added

- `core/identity/`: the identity Component. Domain types (Tenant, Session,
  PendingLink, RemoteIdentity), typed failures (`ErrUnknownFingerprint`,
  `ErrLinkCodeUnknown`), the link-code rules (8-char unambiguous alphabet,
  10-minute lifetime, single-use), and the `LinkAccount` Business Transaction
  (consume code -> redeem grant -> seal token -> bind tenant -> announce),
  behavior-tested at the Component port.
- `adapters/vault/`: NaCl secretbox token vault (`nonce||box` layout, 32-byte
  base64 key, undifferentiated `ErrCannotOpen` on wrong key or tampering),
  plus a key-minting helper for provisioning.
- `kit.Nothing`/`kit.Done`: unit Outcome for effect-only railway steps.
- `adapters/dnsimple/`: the DNSimple driven adapter. `Authenticator`
  implements the identity port (grant exchange + `/whoami` as one railway
  step), counts every API call on the usage metrics, and ships a reusable
  in-process fake for grey-box suites.
- `adapters/bridge/`: the browser leg as a production adapter, driving the
  `LinkAccount` BTX through the identity Component. Grey-box covered end to
  end (real Component, real vault, real authenticator, fake DNSimple),
  including the made-up-code path that must never reach DNSimple.
- `adapters/sshui/`: the SSH driving adapter. Wish + Bubble Tea serve the
  waiting/linked/failed screens, sessions park on `AwaitLink` with the link
  code's own lifetime as timeout, and a metrics middleware (replacing wish's
  command-line-logging one) feeds the session gauge and duration histogram.
- `adapters/memory/`: in-memory identity repository for grey-box tests and
  database-free local hacking.
- `cmd/dnsshimple` now wires the whole M1 stack: Postgres (with boot-time
  migration), the secretbox vault (`DNSSHIMPLE_VAULT_KEY`, boot-refusing when
  absent), the DNSimple authenticator, the bridge, the SSH server, and the
  ops endpoint.
- `adapters/postgres/IdentityRepository`: the identity persistence port on
  Postgres. Atomic single-use link codes (`DELETE ... RETURNING`), tenant
  upsert by DNSimple account with key rebinding, and the cross-instance
  session wake on `LISTEN/NOTIFY` (`dnsshimple_link_completed`), with the
  notify-before-listen race closed by a post-`LISTEN` recheck. Grey-box
  covered, including a two-connection wake test.
- `docker-compose.yml`: local Postgres 17 on port 5433 for development and
  grey-box tests.
- `adapters/postgres/`: the persistence adapter. Boot-time pool with a fail-fast
  ping, and an embedded, transactional, idempotent migrator. Migration `0001`
  creates the identity schema (`tenants`, `ssh_keys`, `pending_links`).

## [0.1.0] - 2026-07-22

Foundation release: the Design Document, the validated auth spike, and the M0
scaffold (kit, telemetry, entrypoint, CI).

### Added

- `kit/`: shared core primitives, starting with the `Outcome[Value]` Result
  monad (`Success`/`Failure`/`Chain`) extracted from the spike, with behavior
  tests. The spike now consumes it.
- `core/telemetry/`: the telemetry Component. JSON logging where every record
  automatically carries the `tenant_id`, `session_id`, and `btx_id` found in
  the context, and the Prometheus registry with the design's instruments
  (active sessions, session duration, DNSimple calls by endpoint/status, 429
  count, and Business Transaction outcomes). Traces are still pending (OTel).
- `cmd/dnsshimple/`: the production service entrypoint. Boots the JSON logger
  (debug level via `DNSSHIMPLE_DEBUG`) and serves the ops endpoint
  (`/metrics`, `/health`) on `DNSSHIMPLE_OPS_ADDRESS` (default
  `localhost:9090`).
- CI (GitHub Actions): `mise`-driven vet, race-enabled tests, and
  `golangci-lint` with the style charter applied (idiom naming checks
  `ST1003`/`ST1006`/`ST1016` disabled, correctness checks kept).
- Sandbox OAuth wiring: the DNSSHimple OAuth application registered at
  sandbox.dnsimple.com (callback `http://localhost:8181/callback`) feeds the
  spike through `mise.local.toml` (git-ignored), so `mise exec -- go run
  ./spike` talks to the real sandbox instead of the local fake. Validated end
  to end on 2026-07-22: sandbox account #2416 linked through a real browser
  consent, and the waiting SSH session woke with the account on screen.
- `DESIGN.md`: the Design Document for the SSH-served DNSimple TUI. Covers the
  Go + Wish + Bubble Tea stack, the hexagonal architecture with 8 Components
  and 6 Business Transactions (BTX), the link-code OAuth flow, the DNSimple
  rate-limit strategy, the observability plan, the grey-box TDD strategy, and
  the code style charter (`Outcome[Value]` errors, semantic naming).
- `mise.toml`: pins Go 1.26, `golangci-lint`, and `gotestsum`.
- `spike/`: a working end-to-end spike of the link-code OAuth approach. An SSH
  session (Wish + Bubble Tea) shows a single-use Link Code; the browser bridge
  runs the DNSimple authorization-code flow (against a local fake, or the real
  sandbox via `DNSIMPLE_CLIENT_ID`/`DNSIMPLE_CLIENT_SECRET`); the callback
  binds the SSH key fingerprint to the account and wakes the waiting session.
  Covered by a grey-box handshake test, green under `go test -race`.
