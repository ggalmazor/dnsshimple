# Changelog

All notable changes to DNSShimple are documented in this file. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
