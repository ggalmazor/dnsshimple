// Package workflows holds the core side of the async workflows (DESIGN.md
// §3.2): the state machines the job runner executes. The decision of what a
// poll outcome means for a workflow is domain policy; adapters/jobs only
// translates the verdict into River verbs.
package workflows

import (
	"errors"
	"time"

	"dnsshimple/core/gateway"
)

// PollStep is a polling workflow's verdict, separated from any job runner
// so the decision logic tests without a database: snooze, fail (the runner
// retries), or done. Shared by every state-polling workflow.
type PollStep struct {
	SnoozeFor time.Duration
	Failure   error
}

// DecidePollStep turns one poll outcome into the workflow's next move.
func DecidePollStep(failure error, terminal bool, now time.Time, pendingSnooze time.Duration) PollStep {
	if failure != nil {
		if rateLimited, isRateLimited := gateway.IsRateLimited(failure); isRateLimited {
			// Honor the budget: sleep to DNSimple's own reset (plus margin)
			// instead of burning retries against a closed door.
			wait := rateLimited.ResumeAt.Sub(now) + 10*time.Second
			if wait < time.Minute {
				wait = time.Minute
			}
			return PollStep{SnoozeFor: wait}
		}
		if errors.Is(failure, gateway.ErrUpstreamDown) {
			return PollStep{SnoozeFor: time.Minute}
		}
		return PollStep{Failure: failure}
	}
	if terminal {
		return PollStep{} // the Component already logged and counted it
	}
	return PollStep{SnoozeFor: pendingSnooze}
}
