# Changelog

All notable changes to DNSShimple are documented in this file. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.21.0] - 2026-07-24

### Added

- Interactive action footers on every list screen: actions wrap into rows
  at a fixed width instead of stretching the frame, `tab`/`shift+tab` cycle
  focus through them (highlighted), `enter` executes the focused action
  against the current list selection, and `up`/`down` hand focus back to
  the list. Direct shortcuts keep working; forms keep `tab` for fields.

### Changed

- The corner wordmark sits flush with the frame bottom - no padding row
  beneath it.

## [0.20.0] - 2026-07-24

### Added

- The certificates list gains the fuzzy filter (`/`, matching domain and
  common name); every action (enter/w/K/i) operates on the filtered list.

### Changed

- The certificates list drops the domain column - the common name already
  carries it.
- Timestamps everywhere render human-readable: "Jul 24, 2026" for dates,
  "Jul 24, 2026 - 10:02 am" for instants (domains renewal, certificate
  expiry, pushes, billing). Unparseable values pass through untouched.

## [0.19.6] - 2026-07-24

### Fixed

- The frame is back to its fixed 26-row height (v0.19.5 misread the
  request), and overflowing lists now window to reach the footer: the list
  chrome estimate shrank from 12 to 7 rows, which was the actual cause of
  the empty dot band below lists.

## [0.19.5] - 2026-07-24

### Changed

- The frame now takes the full terminal height (minus margins), so lists
  fill the available vertical space instead of windowing at 14 rows above a
  field of dots. Screens stay uniformly sized; content still never
  stretches the frame.

## [0.19.4] - 2026-07-24

### Changed

- The frame height is fixed (26 rows, clamped to small terminals): lists
  scroll inside it instead of stretching it, so every screen is now exactly
  the same size. List windows derive from the frame height rather than the
  terminal height.

## [0.19.3] - 2026-07-24

### Changed

- The dot pattern continues through the wordmark's own empty cells, so the
  corner signature sits in the background instead of punching a blank
  rectangle into it.

## [0.19.2] - 2026-07-24

### Changed

- The frame's corner signature is now the dim `dnsimple` ASCII wordmark
  instead of the colored logomark - background decoration should read as
  background. The pixel logomark and its renderer are gone; the art
  generator remains for future use.

## [0.19.1] - 2026-07-24

### Added

- The DNSimple logomark (blue-circle d, from `logomark-classic.svg`) sits
  in the bottom-right corner of the frame's background fill - only when
  enough empty rows exist, so it never crowds a long list. The generator
  now takes a variable name, making it reusable for any of the logos.

## [0.19.0] - 2026-07-24

### Added

- A consistent frame: every screen pads to a 72x26 minimum (clamped to the
  terminal), growing vertically only when a long list needs it, so views
  stop changing size underfoot. The padding carries a low-key dot pattern
  through the session renderer - decoration lives where content is not,
  because text drawn over art is unreadable.
- TLD fuzzy filtering: `/` on the catalog opens a live filter
  (case-insensitive subsequence: "dv" finds .dev), esc clears it, and
  enter still fetches the selected TLD's price.

## [0.18.1] - 2026-07-24

### Fixed

- Analytics no longer fails with "cannot unmarshal object into Go struct
  field": /dns_analytics is the one endpoint whose `data` is an object
  (headers plus rows of values) rather than a list, and the gateway now
  decodes that shape, mapping columns by header name. The fake had mirrored
  the gateway's wrong assumption instead of production - it now serves the
  real envelope, which is what lets the grey-box suite catch this class of
  drift.

## [0.18.0] - 2026-07-24

### Added

- TLD prices: `enter` on a TLD in the catalog fetches its registration
  price and shows it in the header status. On demand rather than for the
  whole list deliberately: prices are one API call per TLD, and pricing
  the full catalog would burn hundreds of requests per view.

### Changed

- Status and error messages moved from the footer to the top of every
  screen, directly under the title - where the eye already is.

## [0.17.1] - 2026-07-24

### Changed

- Trusty is now the hand-drawn smiling map (Guillermo's `trusty_smile.txt`
  with the `render_trusty.sh` palette) instead of the SVG downsample: a
  character map plus an ANSI-256 palette, rendered as background-colored
  cells through each session's renderer, one style per palette entry. The
  source map and script live in `tools/trustyart/` for future edits.

## [0.17.0] - 2026-07-23

The API completionist slice: money before commitment, PEMs in hand.

### Added

- Price-confirmed registrations: submitting the register form now checks
  availability and fetches the registry price first, and the confirm reads
  "example.com is available for $14.50/yr. Register it?" - no more blind
  spending.
- Certificate retrieval: `w` on a certificate shows the full PEM chain in a
  scrollable viewer; `K` shows the private key after an explicit confirm,
  with a reminder to clear scrollback - and the key never lingers in state
  after leaving the viewer.
- Zone files: `f` on a zone shows it in master file format.
- A shared scrollable text viewer backs all three.

## [0.16.0] - 2026-07-23

### Added

- Trusty, DNSimple's mascot, decorates the home and link screens as
  colored half-block art generated from the official SVG
  (`tools/trustyart`). Cells carry hex colors and render through each
  session's renderer, so truecolor terminals get the full mascot and
  colorless ones a clean silhouette - never escape-code soup.

## [0.15.0] - 2026-07-23

M8 complete: the API coverage waves are done.

### Added

- Secondary DNS from the home menu: primary servers (validated create,
  link/unlink with a confirmed unlink - DNSimple stops slaving its zones -
  and confirmed delete) plus secondary zone creation.
- DNS analytics: query volume by zone, sorted by traffic.
- The TLD catalog: every TLD with its registration/transfer/WHOIS-privacy/
  IDN flags and minimum registration years.

With this, every planned DNSimple API area is live in the TUI: identity,
zones (records, batch, activation, distribution), domains (DNSSEC, forwards),
the full registrar, certificates, contacts, templates, services, pushes,
webhooks, secondary DNS, analytics, TLDs, and billing.

## [0.14.0] - 2026-07-23

M8 opens: webhooks and zone operations.

### Added

- Webhooks from the home menu: list, create (https enforced client-side -
  account events over plain HTTP are a leak, not a webhook), and confirmed
  delete.
- Zone activation: the zones list shows each zone's active state, and `x`
  toggles it - deactivation asks for confirmation, because DNSimple's name
  servers stop answering for the zone.
- Zone distribution checks: `D` on a zone asks DNSimple whether it is fully
  propagated across the name server network, answered in the status line.

## [0.13.0] - 2026-07-23

M7 complete: registrant changes - plus two navigation restructures from
feedback.

### Added

- Registrant changes: `c` on a domain's settings opens a contact picker; the
  `ChangeRegistrant` async BTX checks the registry's demands first (TLDs
  requiring extended attributes are refused with a pointer to dnsimple.com),
  then creates the change and polls it gently - every 15 minutes, because
  IRT approvals take days, not seconds.
- Certificates are a top-level home entry: an account-wide list aggregated
  across the portfolio (absorbed by the read cache), with `i` opening a
  domain picker into the usual request form.
- The fourth poll worker rides a per-workflow cadence: registrations and
  certificates poll at 15 s, transfers at 5 min, registrant changes at
  15 min - registry-speed operations no longer spend API budget at
  DNS-speed intervals.

### Changed

- Templates (`p`) and services (`v`) moved from the domains list to the
  zones list - they write records, which is zone territory, and hosted
  zones never appeared in the domains list at all.

## [0.12.0] - 2026-07-22

M7 nearly closes: one-click services and domain pushes.

### Added

- Services: `v` on a domain lists its applied one-click services, `a` opens
  a picker of what is not yet applied (confirmed apply), `d` removes with
  confirmation - removal takes the provider's records out of the zone, and
  the confirm says so.
- Pushes: `g` on a domain's settings pushes it to another DNSimple account
  (validated email, confirmed); the Pushes home screen lists incoming pushes
  with confirmed accept (`a`, registered to your first contact) and reject
  (`x`).

## [0.11.0] - 2026-07-22

M7 continues: templates.

### Added

- Templates from the home menu: create (name, validated short name,
  description), delete (confirmed, takes the records with it), and template
  records - the same validated record form zones use, because template
  records share the zones vocabulary end to end.
- `ApplyTemplate` sync BTX: `p` on a domain opens the template picker, and a
  confirmed apply lands every template record in the zone as one operation.
  Grey-box proven: a two-record template applied to a zone leaves exactly
  those records in it.

## [0.10.0] - 2026-07-22

M7 opens with contacts, plus two fixes straight from live use.

### Added

- Contacts, full CRUD from the home menu: list, a validated 9-field form
  (registry requirements checked client-side: required fields, email shape,
  international phone, 2-letter country), edit, and confirmed delete
  (DNSimple's refusal for in-use contacts passes through verbatim). The
  registrar's "no contacts" errors now point at the Contacts screen instead
  of dnsimple.com.

### Fixed

- `s` on a registered domain no longer fails with "DNSimple says: Not
  Found": DNSimple answers 404 for features never purchased (WHOIS privacy
  on a fresh domain), and the gateway now reads that as "disabled". The fake
  mirrors the 404 so the translation stays grey-box covered.
- `t` on a hosted domain prefills the transfer form with the selected
  domain's name and jumps focus to the auth code.

## [0.9.0] - 2026-07-22

M6 complete: the full domain operations wave.

### Added

- Transfers in: `t` from the domains list opens the transfer form (domain +
  auth code from the losing registrar); the `TransferDomain` async BTX
  registers with the account's first contact and polls registry progress on
  River (first look after a minute - registry transfers move on the scale of
  minutes to days).
- Transfers out: `o` on the settings screen authorizes the transfer away
  (confirmed; the auth code goes to the registrant by email).
- Restores: `u` on the settings screen orders a restore for an expired
  domain (confirmed - it charges the account).
- The fake grew transfer/restore/authorize endpoints (transfers complete on
  the second poll and join the portfolio); grey-box suites cover the round
  trips and the auth-code precondition.

## [0.8.0] - 2026-07-22

M6 slice 3: registrar controls on the settings screen.

### Added

- WHOIS privacy (`w`) and the transfer lock (`l`) - disabling the lock asks
  for confirmation, since unlocking is the precondition for a transfer away.
- Delegation viewing and editing (`e`): comma-separated name servers,
  validated client-side (2-6 well-formed hostnames) before a request is
  spent.
- `RenewDomain` (`R`), a confirmed, metered sync BTX - it charges the
  account, so it asks first and counts in the BTX metrics.
- The fake grew whois/lock/delegation/renewal endpoints; grey-box suites
  cover every round trip.

## [0.7.1] - 2026-07-22

### Fixed

- Colors over SSH: styles now come from a per-session renderer probing the
  client terminal (`bubbletea.MakeRenderer`), because the containerized
  server has no TTY and the global lipgloss renderer silently stripped
  every color and bold. Local `go run` had a TTY, which is why it only
  broke in deployment.
- Form focus is visible: the focused field carries a highlighted `>` prompt
  in every form (records, certificate request, email forward).

## [0.7.0] - 2026-07-22

M6 begins: domain settings from the terminal, and the now-public bridge
prices brute-force attempts.

### Added

- M6 slice 1 (Components and gateways): DNSSEC status and toggle, email
  forwards (list, create with client-side validation, delete), and the
  domain auto-renewal toggle through the registrar. Fake and grey-box
  suites cover all three round trips.
- The domain-settings screen (`s` from the domains list): DNSSEC state with
  `x` to toggle (disabling asks for confirmation - it can break resolution
  for validating resolvers), renewal mode with `a` to toggle, and the email
  forwards list with `n` (form) and `d` (confirmed delete).

### Security

- The bridge caps link-code submissions and callbacks at 10 per client IP
  per 10 minutes (client read from `X-Forwarded-For` behind Caddy). The
  bridge is internet-exposed through the router's 4443 forward; codes are
  40-bit/single-use/10-minute, so brute force cannot win, but it is no
  longer free to attempt.

## [0.6.0] - 2026-07-22

M5: TUI feedback round - full pagination, a centered layout, and a real
certificate request form.

### Added

- API pagination: every list gateway walks DNSimple's pagination envelope to
  the last page (grey-box proven: 150 records across exactly 2 calls).
- TUI windowing: long lists scroll inside a window sized to the terminal,
  with a position indicator and `pgup`/`pgdown`; the whole TUI now centers
  itself vertically and horizontally.
- The certificate request form (common-name subdomain, alternate names,
  auto-renew), reachable with `c` from the domains list and `i` from a
  domain's certificate list; validation failures reopen the form.

### Changed

- The milestone plan now tracks full API coverage in waves (M6 domain
  operations, M7 contacts/templates/services, M8 webhooks/secondary
  DNS/analytics, M9 production).

### Added

- TLS on the LAN bridge: Caddy terminates HTTPS at
  `https://dnsshimple.sendia.org:4443` (port 4443 because Nextcloud AIO owns
  443 on the host) with a Let's Encrypt cert shipped out of band, valid to
  2026-10-20 (manual renewal until DNS-01 is automated). The plain-HTTP
  bridge port is no longer published; browsers reach it only through Caddy.
- Deployment kit for LAN testing (production infra punted for now):
  a multi-stage `Dockerfile` (cross-compiling builder, distroless static,
  nonroot, ~20 MB) and `deploy/` - a target-side compose stack (Postgres +
  app, persistent volumes for data and the SSH host key) and `deploy.sh`,
  which builds the amd64 image, ships it over SSH with
  `docker save | docker load` (no registry), creates the first `.env` with a
  fresh vault key, and composes up with a health check. First deploy to
  192.168.1.130 verified: all listeners up, `/health` answering on the LAN.

## [0.5.0] - 2026-07-22

M4: certificates and billing. Request Let's Encrypt issuance from the
terminal with background polling, and read the account's charges.

### Added

- `core/certificates/`: the certificates Component and the `IssueCertificate`
  async BTX (purchase the Let's Encrypt order, request issuance, queue the
  state poll). Built against the spec fake; the sandbox cannot issue, so live
  verification waits for production (decision of 2026-07-22).
- `core/billing/`: account charges read path.
- `adapters/jobs/`: the certificate poll worker, sharing the generalized poll
  decision (rate-limit snooze, breaker backoff, River retries) with
  registrations.
- The fake grew Let's Encrypt purchase/issue/poll endpoints (issued on the
  second poll) and billing charges.
- TUI: domains open into their certificate list (`enter`), `i` requests a
  Let's Encrypt certificate, and the home menu gains Billing.

## [0.4.1] - 2026-07-22

Fix release: actionable errors, the registration timeout, test isolation,
CI actually testing what it claims, and a stack refresh.

### Added

- `README.md`: what DNSShimple is, what works today, how to run it, and the
  configuration surface. Kept updated alongside the changelog.

### Changed

- Stack refresh (2026-07-22): every Go module bumped to its latest version
  (notably `charmbracelet/log` v1.0.0); Go stays on 1.26.5 (newest release).
  Full suite green, govulncheck clean except the accepted openpgp advisory.

### Fixed

- The Postgres grey-box suite no longer wipes the developer's real data: it
  pointed at the same tables the dev server uses and truncated `tenants`
  (and with it every linked SSH key) on each run, forcing a fresh OAuth link
  after every test run. The suite now runs inside its own Postgres schema
  (`dnsshimple_tests`, pinned via `search_path` and created on demand),
  fails loudly if `current_schema()` is anything else before running a
  destructive statement, and the compose Postgres gained a named volume so
  dev data survives container recreation.
- Registration POSTs no longer die at 15 s while DNSimple completes them
  server-side: deadlines are now per-call in the gateway client (15 s reads,
  90 s registration writes) instead of one blanket `http.Client` timeout.
- CI was green while testing less than advertised: `CGO_ENABLED=0` broke
  `-race` on Linux, and without a database every Postgres suite silently
  skipped. Fixed (cgo restored, Postgres 17 service container), plus the
  Dependabot action bumps (checkout v7, mise-action v4) applied directly.
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
