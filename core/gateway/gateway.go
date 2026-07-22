// Package gateway holds the failure vocabulary of the DNSimple gateway
// (DESIGN.md §5), so every Component and screen can branch on what the
// upstream did without knowing how the client works.
package gateway

import (
	"errors"
	"fmt"
	"time"
)

// RateLimitedError says the tenant's DNSimple budget is exhausted; nothing
// will succeed before ResumeAt, so callers should tell the user and stop
// trying instead of burning the connection.
type RateLimitedError struct {
	ResumeAt time.Time
}

func (rateLimited RateLimitedError) Error() string {
	return fmt.Sprintf("gateway: DNSimple rate limit exhausted, resuming at %s",
		rateLimited.ResumeAt.Format(time.Kitchen))
}

// ErrUpstreamDown says the circuit is open: DNSimple keeps failing and the
// gateway is failing fast instead of feeding a degraded upstream.
var ErrUpstreamDown = errors.New("gateway: DNSimple keeps failing; backing off")

// UpstreamRejectionError carries DNSimple's own message for a 4xx: "TLD
// .term is invalid" is actionable where a raw error chain is not.
type UpstreamRejectionError struct {
	StatusCode int
	Message    string
}

func (rejection UpstreamRejectionError) Error() string {
	return fmt.Sprintf("gateway: DNSimple rejected the request (%d): %s",
		rejection.StatusCode, rejection.Message)
}

// UpstreamTimeoutError is honest about what a timeout means: the answer
// never arrived, but the operation may still have completed server-side.
type UpstreamTimeoutError struct {
	Operation string
}

func (timeout UpstreamTimeoutError) Error() string {
	return "gateway: DNSimple did not answer " + timeout.Operation +
		" in time; the operation may still have completed"
}

// IsRateLimited unwraps the typed rate-limit failure, if that is what err is.
func IsRateLimited(err error) (RateLimitedError, bool) {
	var rateLimited RateLimitedError
	if errors.As(err, &rateLimited) {
		return rateLimited, true
	}
	return RateLimitedError{}, false
}
