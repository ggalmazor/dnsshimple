# Components

Normative rules for components. Rationale and the components map live in
[../DESIGN.md](../DESIGN.md) §3; this file is what a change must obey.

## Definition

A component is a package under `core/` owning one business slice (vertical)
or one infrastructure concern (horizontal: `gateway`, `telemetry`). It
exposes a facade (`Component` struct with methods), its types, and its
errors - nothing else. Callers never reach past the facade.

## Rules

1. The core never imports an adapter. Adapters implement core-owned port
   interfaces (`Gateway`, `Repository`, `PollQueue`, ...).
2. Components may compose other components, but only through a narrow port
   the consumer declares and the constructor injects (see
   `domains.RegistrarSettings`) - never by importing the other component's
   package from core code. This keeps core-to-core imports out and makes
   the dependency visible in `cmd/dnsshimple/main.go`.
3. All DNSimple traffic rides the rate-limited client through a gateway
   port. No component holds an HTTP client.
4. Every facade method takes `ctx context.Context` and (when tenant-scoped)
   `identity.Session` as its first arguments and returns `kit.Outcome[T]`.
   Plain `(value, error)` exists only at adapter boundaries.
5. Ownership: exactly one component owns each API area (the map in DESIGN
   §3.1 is authoritative) and each Postgres table. Complex reads over a
   component's data belong to that component as named facade methods.
6. Adapter-level operations (DESIGN §8.6): an API operation no component
   needs stays a method on the concrete gateway - fake-backed and
   contract-checked - and is promoted onto a component port the moment its
   first core caller appears.

## Boundary tests

- Facade test: can a caller do its job knowing only the facade, its types,
  and its errors? If it needs an adapter type or an internal, the facade
  leaks (or the operation belongs to the adapter tier).
- Ownership test: does any code outside the owning component query or
  mutate its tables or API area? Move it behind the facade.
- Placement test: does the components map name an owner for the API area
  being touched? If the map is silent or contradicts the code, fix the map
  in the same change - the map must never drift again.
