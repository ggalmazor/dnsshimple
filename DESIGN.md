# DNSShimple - Design Document

Status: draft v0.1 (2026-07-22)

DNSShimple is a multi-tenant TUI served over SSH, in the style of terminal.shop.
Users `ssh dnsshimple.com`, link their DNSimple account once via OAuth, and then
manage their DNSimple assets (domains, zones, records, certificates, registrar
operations) from a terminal UI.

## 1. Goals and constraints

- Multi-tenant: many concurrent users, strict per-tenant isolation of data and tokens.
- Light footprint: small image, low idle memory, cheap per-session cost, fast cold start.
- Cloud-first: stateless app tier, horizontal scaling, single-container deploy.
- DNSimple API is rate-limited (2400 req/hour per token, `X-RateLimit-*` headers,
  429 responses): the client must degrade gracefully, never hammer.
- TDD workflow with grey-box tests as the default test style.
- Hexagonal architecture (ports and adapters) as the overall approach.
- Components model cohesive business/infrastructure slices. Business transactions
  (BTX) and async workflows model business operations and orchestrate calls across
  components. Components can call other components.
- Operability: structured error logs, debug traces, and usage metrics from day one.
- Tooling pinned with `mise` (languages and dev tools).

## 2. Tech stack

| Concern | Choice | Reason |
|---|---|---|
| Language | Go (pinned via `mise`) | Static binary, tiny image, goroutine-per-session, interfaces map 1:1 to ports |
| SSH server | `charmbracelet/wish` | Purpose-built middleware server for serving Bubble Tea apps over SSH; proven by terminal.shop |
| TUI | `charmbracelet/bubbletea` + `bubbles` + `lipgloss` | Elm-architecture TUI; testable with `teatest` |
| HTTP (auth bridge) | `net/http` stdlib | One small HTTPS listener for the OAuth callback; no framework needed |
| Storage | PostgreSQL (managed) | Multi-tenant rows, LISTEN/NOTIFY for auth handoff, shared by all app instances |
| Async jobs | `riverqueue/river` | Postgres-backed queue: transactional enqueue, retries, no extra infra |
| DNSimple client | Generated from `openapi.yml` + hand-written gateway wrapper | Spec is the contract; wrapper adds rate limiting, caching, and telemetry |
| Logs | `log/slog` (JSON) | Stdlib, structured, zero deps |
| Traces | OpenTelemetry SDK | Spans from TUI event through BTX to DNSimple HTTP call |
| Metrics | Prometheus (`/metrics`) | Sessions, latencies, DNSimple call budget, queue depth |
| Secrets crypto | NaCl secretbox (`golang.org/x/crypto`) | Encrypt OAuth tokens at rest; key from env/KMS |
| Deploy | Docker (distroless or scratch) on Fly.io or any container host | ~15-25 MB image; raw TCP anycast for SSH works on Fly |

Dev-mode alternative: SQLite for local hacking is allowed behind the storage port,
but CI and production run Postgres. Horizontal scaling requires the shared DB.

## 3. Architecture (hexagonal)

```
                 driving adapters                      driven adapters
  ┌──────────────┐                                          ┌───────────────────┐
  │ SSH / TUI    │──┐                                   ┌──▶│ DNSimple gateway  │──▶ api.dnsimple.com
  │ (wish+tea)   │  │   ┌───────────────────────────┐   │   │ (rate-limit aware)│
  └──────────────┘  ├──▶│  application core         │───┤   └───────────────────┘
  ┌──────────────┐  │   │  components + BTX +       │   │   ┌───────────────────┐
  │ OAuth bridge │──┘   │  async workflows          │   ├──▶│ Postgres store    │
  │ (HTTPS)      │      │  (pure domain + ports)    │   │   └───────────────────┘
  └──────────────┘      └───────────────────────────┘   │   ┌───────────────────┐
  ┌──────────────┐                  ▲                   └──▶│ token vault       │
  │ River worker │──────────────────┘                       │ (secretbox)       │
  │ (job runner) │                                          └───────────────────┘
  └──────────────┘
```

Rules:

- The core never imports an adapter. Adapters implement core-owned port interfaces.
- The TUI is a driving adapter: it renders core state and dispatches core operations.
  No business logic in Bubble Tea models.
- All DNSimple traffic goes through the gateway component. No component holds an
  HTTP client of its own.

### 3.1 Components (cohesive slices)

Each component owns its domain types, its port interfaces, and its slice of the
schema. Components can call other components through their public interfaces.

| Component | Responsibility | DNSimple API areas |
|---|---|---|
| `identity` | SSH pubkey ↔ tenant mapping, OAuth link flow, token vault, whoami | `/oauth/access_token`, `/whoami`, `/accounts` |
| `gateway` | Rate-limit-aware DNSimple HTTP client, per-token budget, read cache, circuit breaker | all (infrastructure slice) |
| `domains` | Domain list/detail, DNSSEC, DS records, email forwards, pushes | `/{account}/domains/**` |
| `registrar` | Check, prices, register, transfer, renew, restore, delegation, transfer lock, WHOIS privacy, registrant changes | `/{account}/registrar/**`, `/tlds/**` |
| `zones` | Zones, records CRUD, batch record changes, zone file, distribution checks | `/{account}/zones/**` |
| `certificates` | Let's Encrypt purchase, issue, renewal, download | `/{account}/domains/{d}/certificates/**` |
| `billing` | Charges listing | `/{account}/billing/charges` |
| `telemetry` | Logs, traces, metrics plumbing (infrastructure slice) | n/a |

Deferred (post-v1): contacts, templates, services, secondary DNS, webhooks,
vanity NS, DNS analytics. The spec covers them; the component pattern extends to
them without structural change.

### 3.2 Business transactions and async workflows

A BTX is a named, logged, traced unit of business work that orchestrates one or
more components. Sync BTX run in the session goroutine; async workflows are
persisted state machines executed by River jobs (survive restarts, retry with
backoff, resume after redeploys).

Initial catalog:

| Name | Kind | Orchestrates |
|---|---|---|
| `LinkAccount` | async | identity: issue link code -> wait for OAuth callback -> exchange code -> vault token -> notify session (LISTEN/NOTIFY) |
| `RegisterDomain` | async | registrar: check -> price -> create registration -> poll registration state -> domains: refresh |
| `TransferDomain` | async | registrar: initiate -> poll transfer state -> notify |
| `IssueCertificate` | async | certificates: purchase -> issue -> poll until issued -> ready to download |
| `BatchEditRecords` | sync | zones: validate set -> `/zones/{zone}/batch` -> distribution check |
| `RenewDomain` | sync | registrar: renewal -> confirmation |

Async workflow mechanics: one `workflows` table (id, tenant, type, state, payload,
updated_at) plus River jobs per step. Polling steps honor the gateway budget and
reschedule instead of spinning when the budget is low.

## 4. Auth: OAuth without a browser

DNSimple supports only the authorization-code grant (confirmed in `openapi.yml`:
`/oauth/access_token`, no device-grant endpoint). An SSH session has no browser,
so we emulate the device flow with a link code plus a minimal HTTPS bridge.

Flow:

1. Session opens. Wish gives us the SSH public key. Fingerprint -> tenant lookup.
2. Known key: load tenant, decrypt token, show the main UI. Done.
3. Unknown key: show a link screen: `https://dnsshimple.com/link` + 8-char code
   (e.g. `KZ3Q-7HWP`, ~40 bits, single-use, 10-minute TTL, rate-limited attempts).
4. User opens the URL in any browser, enters the code. The bridge starts the
   standard OAuth redirect to `dnsimple.com/oauth/authorize` with `state` bound
   to the link code (CSRF-safe).
5. Callback exchanges the code at `/oauth/access_token`, encrypts the token
   (secretbox), stores tenant + pubkey + token, and fires Postgres NOTIFY.
6. The waiting TUI (listening on the channel) unblocks and enters the account UI.
7. Returning connections authenticate by pubkey alone. No browser ever again.

Token lifecycle: DNSimple OAuth tokens do not expire (revocable by the user). On
a 401 from the API, mark the tenant unlinked and re-run the link flow. Support
multiple pubkeys per tenant (laptop + desktop) and an explicit `unlink` command.

Fallback (hidden, discouraged): paste a DNSimple account API token directly. Same
vault path. Useful for CI-like usage and users who refuse the browser hop.

## 5. DNSimple rate limiting

Budget: 2400 requests/hour per token. Responses carry `X-RateLimit-Limit`,
`X-RateLimit-Remaining`, and `X-RateLimit-Reset`; 429 on exhaustion.

Gateway strategy (all inside the `gateway` component, invisible to callers):

1. Per-token budget tracker seeded from response headers (not guessed client-side).
2. Soft-limit throttle: below a remaining threshold (default 10%), queue
   non-interactive calls (workflow polls) and only pass interactive ones.
3. 429 handling: never retry immediately; sleep until `X-RateLimit-Reset` with
   jitter; surface a literal "rate limited by DNSimple, resuming at HH:MM" line
   in the TUI status bar.
4. Read cache: short-TTL (30-60 s) per-tenant cache for list/detail GETs;
   invalidate on any write to the same resource. Most TUI navigation is re-reads.
5. Request coalescing: concurrent identical GETs within a tenant share one flight
   (`singleflight`).
6. Circuit breaker on repeated 5xx: fail fast with a clear TUI message instead of
   burning budget on a degraded upstream.
7. Metrics: remaining budget gauge, 429 counter, cache hit ratio (see §7).

## 6. Multi-tenancy and security

- Tenant = linked DNSimple account. Every row carries `tenant_id`; every query is
  tenant-scoped through the storage port (no raw SQL in components).
- Tokens encrypted at rest with NaCl secretbox; data key from env/KMS, rotatable
  (key id stored beside ciphertext).
- SSH host key is a deploy secret shared by all instances (users must not see
  host-key-changed warnings when we scale).
- Per-session and per-IP connection limits, idle timeout, and link-code attempt
  rate limiting at the Wish middleware layer.
- No DNSimple data cached to disk; cache is in-memory and per-instance.

## 7. Observability

- Logs: `slog` JSON to stdout. Every record carries `tenant_id`, `session_id`,
  `btx_id`, and `request_id`. Errors logged once, at the boundary, with the
  wrapped cause chain; no double logging.
- Traces: OTel spans - `session -> tui event -> btx -> component op -> gateway HTTP`.
  Exported OTLP; sampled (100% for errors, 10% baseline, tunable by env).
- Debug traces: `DEBUG` log level togglable per tenant at runtime (ops flag in
  DB), so one noisy user can be diagnosed without global log spam.
- Metrics (Prometheus `/metrics`): active sessions, session duration, TUI command
  latency, BTX started/completed/failed by type, DNSimple calls by endpoint and
  status, rate-limit remaining, 429 count, cache hit ratio, River queue depth,
  workflow retry counts.

## 8. Testing strategy (TDD, grey-box first)

Grey-box means: exercise a component or BTX through its public port with real
infrastructure at the near edge and a controlled fake at the far edge, then
assert on observable outcomes (returned state, DB rows, emitted events, recorded
HTTP calls) - not on internal calls.

Layers:

1. Domain core: plain unit tests (pure functions, state machines). Fast, no IO.
2. Component grey-box: real Postgres (testcontainers or a CI service container),
   fake DNSimple as an `httptest.Server` that implements the endpoints from
   `openapi.yml`, including `X-RateLimit-*` headers, 429s, and error payloads.
   Tests drive the component port and assert outcomes.
3. BTX/workflow grey-box: same harness plus River in test mode; assert workflow
   state transitions, retries under injected 429s, and resumption after
   simulated restarts.
4. TUI: `teatest` golden-file tests - feed key events, assert rendered frames.
   TUI tests use a fake core port; logic lives below anyway.
5. Contract check (done, v0.26.0): every grey-box exchange in the dnsimple
   adapter package is validated against the vendored spec
   (`adapters/dnsimple/testdata/openapi.yml`) via `newContractServer`, with
   `required` lists stripped - the fake may say less than production, never
   something production could not say. Documented exemptions only where the
   spec contradicts observed production behavior.

TDD loop: red (failing grey-box test at the port) -> green (minimal
implementation) -> refactor. New behavior starts with a test at the outermost
sensible boundary; unit tests appear only where domain logic gets intricate.

## 9. Deployment and scaling

- Build: single static Go binary; multi-stage Docker to distroless. Image ~20 MB.
- One process serves: SSH (2222), HTTPS auth bridge (443), metrics (9090,
  internal), and embedded River workers. Split workers into a separate process
  only if job load ever justifies it.
- Scale-out: instances are stateless (state in Postgres, cache is per-instance
  best-effort). SSH is plain TCP: any L4 load balancer works; Fly.io anycast TCP
  handles this natively. Shared host key keeps fingerprints stable.
- Capacity model: a Bubble Tea session is a goroutine plus buffers (~tens of KB).
  A 256 MB instance comfortably holds thousands of idle sessions; scale on CPU
  and concurrent-render load, not memory.

## 10. Tooling

`mise.toml` pins Go and dev tools (golangci-lint, gotestsum). CI runs
`mise install` then `gotestsum ./...` against a Postgres service container.

## 11. Milestones

Done: M0-M8 - every planned API coverage wave. Remaining: M9 (production).

Toward full API coverage (instruction of 2026-07-22), in waves:

6. M6 - domain operations suite: DNSSEC + DS records, email forwards,
   transfers (in and out), renewals, restores, auto-renew toggle, WHOIS
   privacy, transfer lock, delegation and vanity NS.
7. M7 - people and reuse: contacts CRUD, registrant changes, templates
   (CRUD + apply), one-click services, domain pushes.
8. M8 - operations and insight: webhooks CRUD, secondary DNS, zone
   activation/distribution checks, DNS analytics, TLD browsing.
9. M9 - production: real infra, DNS-01 cert automation, ops auth, OTel
   export, load test.

## 12. Code style charter

We deliberately deviate from Go's folk conventions where they hurt readability.

- Naming: full semantic names everywhere. No single-letter locals or receivers
  (`session`, not `s`; `store`, not `st`). No enforced initialism casing
  (`Id`/`Url` acceptable). golangci-lint configured to drop `revive`
  var-naming and stutter rules. Compiler-enforced rules remain (exported =
  capitalized; lowercase package names).
- Errors: `Outcome[Value]` (a minimal Result monad: Success/Failure/Chain) in
  the application core, so business steps return one value and failures
  short-circuit railway-style. Known limits, accepted: Go methods cannot add
  type parameters, so cross-type Chain is a free function (nested, not fluent);
  there is no `?` operator. Adapters (net/http, database drivers, Wish) speak
  `(value, error)` natively and convert at the boundary. Domain failures are
  typed error values, never bare `fmt.Errorf` strings, so the TUI can render
  them meaningfully. No panic-based control flow.
- Testing: behavior-based grey-box (see §8). Tests read Given/When/Then, are
  named after the behavior (`TestBrowserHandshakeLinksAnSSHFingerprintToADNSimpleAccount`),
  drive public ports, and assert observable outcomes only. `go test -race`
  always on; stdlib `testing` runner (no BDD DSL framework), `gotestsum` for
  output.

## 13. Open questions

1. ~~OAuth client registration~~ Resolved 2026-07-22: the DNSSHimple sandbox
   app exists (callback `http://localhost:8181/callback`); the spike validated
   the full link flow against it. A production app registration is still
   pending.
2. Sandbox: OAuth and `/whoami` parity confirmed by the spike (2026-07-22);
   registrar parity remains unverified. The sandbox does not support
   certificate issuance at all, so the certificates component is built against
   the OpenAPI spec and its grey-box fake, and verified in production.
3. Multiple DNSimple accounts per user token (`/accounts` returns a list):
   v1 UX picks one account per session; revisit account switching.
4. Terminal capability floor: minimum size and color support we commit to.
