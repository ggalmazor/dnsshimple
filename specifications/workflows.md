# Async workflows

## Definition

An async workflow is a persisted state machine: a BTX accepts the work and
enqueues it; River executes it on Postgres so it survives restarts and
redeploys; a poll worker advances it until a terminal state.

## Rules

1. Policy lives in core, execution in the adapter: `core/workflows` owns
   what a poll outcome means (`DecidePollStep`: terminal completes, rate
   limit snoozes to DNSimple's own reset plus margin, upstream-down backs
   off a minute, anything else fails into the runner's retry).
   `adapters/jobs` only translates verdicts into River verbs.
2. Components enqueue through a core-owned queue port (`PollQueue`);
   `jobs.Queue.Bind` breaks the worker-component construction cycle.
3. Poll cadences are declared at the worker call site: registrations and
   certificates 15s, transfers 5min, registrant changes 15min.
4. Workers load their session via `identity.SessionForTenant` - workflows
   outlive SSH sessions and must never depend on one.
5. Polling honors the gateway budget: a rate-limited poll reschedules to
   the budget's reset; it never burns retries against a closed door.
