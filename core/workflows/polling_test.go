package workflows

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
	limited := DecidePollStep(gateway.RateLimitedError{ResumeAt: resumeAt}, false, now, 15*time.Second)
	if limited.SnoozeFor != 40*time.Minute+10*time.Second {
		test.Fatalf("rate-limited snooze: %v", limited.SnoozeFor)
	}

	// A reset already in the past still waits the minimum, never busy-loops
	stale := DecidePollStep(gateway.RateLimitedError{ResumeAt: now.Add(-time.Minute)}, false, now, 15*time.Second)
	if stale.SnoozeFor != time.Minute {
		test.Fatalf("stale-reset snooze: %v", stale.SnoozeFor)
	}

	// An open circuit backs off instead of consuming River retries
	down := DecidePollStep(gateway.ErrUpstreamDown, false, now, 15*time.Second)
	if down.SnoozeFor != time.Minute || down.Failure != nil {
		test.Fatalf("breaker step: %+v", down)
	}

	// Any other failure goes to River's retry policy
	broken := errors.New("decoding the DNSimple response: unexpected EOF")
	failed := DecidePollStep(broken, false, now, 15*time.Second)
	if !errors.Is(failed.Failure, broken) || failed.SnoozeFor != 0 {
		test.Fatalf("failure step: %+v", failed)
	}

	// In-flight keeps polling; terminal ends the workflow
	pending := DecidePollStep(nil, false, now, 15*time.Second)
	if pending.SnoozeFor != 15*time.Second {
		test.Fatalf("pending step: %+v", pending)
	}
	done := DecidePollStep(nil, true, now, 15*time.Second)
	if done.SnoozeFor != 0 || done.Failure != nil {
		test.Fatalf("terminal step: %+v", done)
	}
}
