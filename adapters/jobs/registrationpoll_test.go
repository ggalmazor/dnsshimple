package jobs

import (
	"errors"
	"testing"
	"time"

	"dnsshimple/core/gateway"
)

func TestThePollStepHonorsRateLimitsBreakersAndTerminalStates(test *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	// A rate limit snoozes to DNSimple's own reset plus margin
	resumeAt := now.Add(40 * time.Minute)
	limited := decidePollStep(gateway.RateLimitedError{ResumeAt: resumeAt}, false, now, 15*time.Second)
	if limited.snoozeFor != 40*time.Minute+10*time.Second {
		test.Fatalf("rate-limited snooze: %v", limited.snoozeFor)
	}

	// A reset already in the past still waits the minimum, never busy-loops
	stale := decidePollStep(gateway.RateLimitedError{ResumeAt: now.Add(-time.Minute)}, false, now, 15*time.Second)
	if stale.snoozeFor != time.Minute {
		test.Fatalf("stale-reset snooze: %v", stale.snoozeFor)
	}

	// An open circuit backs off instead of consuming River retries
	down := decidePollStep(gateway.ErrUpstreamDown, false, now, 15*time.Second)
	if down.snoozeFor != time.Minute || down.failure != nil {
		test.Fatalf("breaker step: %+v", down)
	}

	// Any other failure goes to River's retry policy
	broken := errors.New("decoding the DNSimple response: unexpected EOF")
	failed := decidePollStep(broken, false, now, 15*time.Second)
	if !errors.Is(failed.failure, broken) || failed.snoozeFor != 0 {
		test.Fatalf("failure step: %+v", failed)
	}

	// In-flight keeps polling; terminal ends the workflow
	pending := decidePollStep(nil, false, now, 15*time.Second)
	if pending.snoozeFor != 15*time.Second {
		test.Fatalf("pending step: %+v", pending)
	}
	done := decidePollStep(nil, true, now, 15*time.Second)
	if done.snoozeFor != 0 || done.failure != nil {
		test.Fatalf("terminal step: %+v", done)
	}
}
