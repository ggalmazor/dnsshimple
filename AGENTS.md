# Working on DNSShimple

Guidance for coding agents and new contributors. The architecture
rationale lives in [DESIGN.md](DESIGN.md); the normative rules for any
change live in [specifications/](specifications/) - read the spec that
covers your change before writing code.

## Toolchain

Everything is pinned with [mise](https://mise.jdx.dev); prefix toolchain
commands with `mise exec --`.

```sh
mise install                          # Go toolchain, golangci-lint, gotestsum
docker compose up -d                  # local Postgres 17 on :5433
mise exec -- go test -race ./...      # the full suite (13 packages)
mise exec -- golangci-lint run ./...  # lint (charter config in .golangci.yml)
mise exec -- go run ./cmd/dnsshimple  # run locally; ssh -p 2222 localhost
mise exec -- ./deploy/deploy.sh       # ship to the LAN test host
```

Secrets live in git-ignored `mise.local.toml`. Do not set a global
`CGO_ENABLED=0` (it breaks `-race` on Linux CI).

## The per-slice pipeline

Every cohesive change ships the same way, in order:

1. Grey-box test first (see [specifications/testing.md](specifications/testing.md)).
2. `gofmt`/`goimports`, build, full `-race` suite, lint - all green.
3. `CHANGELOG.md` entry (Keep a Changelog; Unreleased at top).
4. Commit in small cohesive chunks. Messages are why-first prose: the
   problem and its mechanism, then the change; "we" for shared work;
   quantified claims with their source. No filler, no bullet-list bodies.
5. Cut a semver tag when the change is user-meaningful; `git push
   --follow-tags`, then deploy to the LAN host and verify `health: ok`.

## Architecture in one paragraph

Hexagonal: components under `core/` own business slices behind facades
returning `kit.Outcome[T]`; adapters implement core-owned ports
(`adapters/dnsimple` is the rate-limited API client + gateways + the
stateful fake; `adapters/sshui` the Wish/Bubble Tea TUI, split by screen
family; `adapters/bridge` the OAuth browser leg; `adapters/postgres`
persistence; `adapters/jobs` River workers executing core-owned workflow
policy). Business Transactions orchestrate; drivers render and dispatch.
The components map (DESIGN §3.1) is authoritative for ownership - if your
change contradicts it, fix the map in the same change.

## Sharp edges worth knowing

- The DNSimple fake must mirror production, not our assumptions; the
  contract check enforces it. When production and the spec disagree,
  trust observed production, exempt narrowly, and file the spec bug.
- lipgloss must render through the per-session renderer (the server
  process has no TTY); never the global one.
- Registration writes can be slow: per-call deadlines, not blanket
  client timeouts.
- The sandbox cannot issue certificates; that flow verifies in
  production only.
