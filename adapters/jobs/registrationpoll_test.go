package jobs

import (
	"errors"
	"testing"
	"time"

	"dnsshimple/core/gateway"
	"dnsshimple/core/registrar"
	"dnsshimple/kit"
)

func TestThePollStepHonorsRateLimitsBreakersAndTerminalStates(test *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	// A rate limit snoozes to DNSimple's own reset plus margin
	resumeAt := now.Add(40 * time.Minute)
	limited := decidePollStep(
		kit.Failure[registrar.Registration](gateway.RateLimitedError{ResumeAt: resumeAt}), now)
	if limited.snoozeFor != 40*time.Minute+10*time.Second {
		test.Fatalf("rate-limited snooze: %v", limited.snoozeFor)
	}

	// A reset already in the past still waits the minimum, never busy-loops
	stale := decidePollStep(
		kit.Failure[registrar.Registration](gateway.RateLimitedError{ResumeAt: now.Add(-time.Minute)}), now)
	if stale.snoozeFor != time.Minute {
		test.Fatalf("stale-reset snooze: %v", stale.snoozeFor)
	}

	// An open circuit backs off instead of consuming River retries
	down := decidePollStep(kit.Failure[registrar.Registration](gateway.ErrUpstreamDown), now)
	if down.snoozeFor != time.Minute || down.failure != nil {
		test.Fatalf("breaker step: %+v", down)
	}

	// Any other failure goes to River's retry policy
	broken := errors.New("decoding the DNSimple response: unexpected EOF")
	failed := decidePollStep(kit.Failure[registrar.Registration](broken), now)
	if !errors.Is(failed.failure, broken) || failed.snoozeFor != 0 {
		test.Fatalf("failure step: %+v", failed)
	}

	// In-flight keeps polling; terminal ends the workflow
	pending := decidePollStep(kit.Success(registrar.Registration{State: "registering"}), now)
	if pending.snoozeFor != 15*time.Second {
		test.Fatalf("pending step: %+v", pending)
	}
	done := decidePollStep(kit.Success(registrar.Registration{State: registrar.StateRegistered}), now)
	if done.snoozeFor != 0 || done.failure != nil {
		test.Fatalf("terminal step: %+v", done)
	}
}
