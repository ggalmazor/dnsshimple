# DNSShimple

Manage your DNSimple domains, zones, and records from a terminal - no client
to install, because the client is `ssh`:

```
ssh -p 2222 dnsshimple.example
```

On first connect the TUI shows a one-time link code; you enter it in a
browser, approve DNSShimple at DNSimple (OAuth), and return to a linked
terminal. Every later connection authenticates by your SSH key alone.

The idea follows [terminal.shop](https://terminal.shop): a real service
living entirely behind an SSH session.

## What works today (v0.24.x)

- Link a DNSimple account over OAuth without leaving the terminal flow;
  reconnect by key, from any number of machines.
- Zones: browse zones and records, create, edit, and delete records, apply
  templates and one-click services, check record distribution.
- Domains: browse the portfolio; register, renew, restore, and transfer
  domains - each runs as a background workflow that survives restarts;
  registrant changes and in-flight transfers are listed and cancellable;
  settings cover DNSSEC, DS records, WHOIS privacy, transfer lock,
  delegation, vanity name servers, and email forwards.
- Certificates: list a domain's certificates and request Let's Encrypt
  issuance (same background-workflow machinery; live flow verifies in
  production, since the sandbox cannot issue).
- Contacts, templates, pushes, webhooks, Secondary DNS, DNS analytics,
  TLD pricing, and the account's Billing charges.
- Every list takes a `/` fuzzy filter; footers are interactive
  (tab cycles actions, enter executes).
- Multi-tenant by construction: tokens sealed with NaCl secretbox at rest,
  every query tenant-scoped, sessions wake across instances via
  Postgres LISTEN/NOTIFY.
- DNSimple's rate limit (2400 requests/hour/token) handled in one place:
  budget tracked from DNSimple's own headers, client-side refusal when
  exhausted, a 45 s read cache, single-flighted reads, and a circuit breaker.

## Running it

Requirements: [mise](https://mise.jdx.dev) and Docker.

```sh
mise install                  # pinned Go toolchain and dev tools
docker compose up -d          # local Postgres 17 on :5433
mise exec -- go run ./cmd/dnsshimple
ssh -p 2222 localhost         # in another terminal
```

Configuration is environment-driven (see `cmd/dnsshimple/main.go`):

| Variable | Purpose | Default |
|---|---|---|
| `DNSIMPLE_CLIENT_ID` / `DNSIMPLE_CLIENT_SECRET` | OAuth application | required |
| `DNSSHIMPLE_VAULT_KEY` | 32-byte base64 key sealing tokens at rest | required (boot refuses without it) |
| `DATABASE_URL` | Postgres | local compose |
| `DNSIMPLE_API_URL` / `DNSIMPLE_AUTHORIZE_URL` | DNSimple endpoints | sandbox |
| `DNSSHIMPLE_SSH_ADDRESS` / `DNSSHIMPLE_BRIDGE_ADDRESS` / `DNSSHIMPLE_OPS_ADDRESS` | listeners | `localhost:2222` / `:8181` / `:9090` |
| `DNSSHIMPLE_PUBLIC_BRIDGE_URL` | public URL of the link bridge | `http://<bridge address>` |
| `DNSSHIMPLE_DEBUG` | debug logging | off |

Secrets live in a git-ignored `mise.local.toml`. The ops endpoint serves
Prometheus metrics at `/metrics` and a liveness probe at `/health`.

## Deploying to the LAN test host

`mise exec -- ./deploy/deploy.sh` builds the amd64 image, ships it to the
target over SSH (`docker save | docker load`, no registry), and composes up
Postgres plus the app with persistent volumes. Target and paths are in
`deploy/deploy.sh` (`DEPLOY_TARGET` overrides); secrets live in a `.env` on
the target, created on first deploy and then left alone because it holds the
vault key. Caddy terminates TLS (`deploy/Caddyfile`); the cert and key are
copied to `certs/` on the target out of band. Condition: linking needs a
DNSimple OAuth app whose callback matches
`DNSSHIMPLE_PUBLIC_BRIDGE_URL` + `/callback`.

## Architecture, briefly

Hexagonal: a Go application core (Components such as `identity`, `zones`,
`registrar`, plus Business Transactions like `LinkAccount` and
`RegisterDomain`) surrounded by adapters - Wish/Bubble Tea for the SSH TUI,
a small HTTP bridge for the OAuth leg, Postgres for state, River for async
workflows, and one rate-limited client in front of the DNSimple API. The
full design, including the tradeoffs, lives in [DESIGN.md](DESIGN.md);
changes are tracked in [CHANGELOG.md](CHANGELOG.md).

Development follows TDD with grey-box tests: suites drive Components through
their public ports against real Postgres and a stateful in-process DNSimple
fake, and assert only on observable outcomes. `mise exec -- gotestsum ./...`
runs everything; CI adds `golangci-lint` and `govulncheck`.
