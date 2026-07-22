# Changelog

All notable changes to DNSShimple are documented in this file. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the project
does not have versioned releases yet, so everything lives under Unreleased.

## [Unreleased]

### Added

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
