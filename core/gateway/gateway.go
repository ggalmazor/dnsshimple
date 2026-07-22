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

// IsRateLimited unwraps the typed rate-limit failure, if that is what err is.
func IsRateLimited(err error) (RateLimitedError, bool) {
	var rateLimited RateLimitedError
	if errors.As(err, &rateLimited) {
		return rateLimited, true
	}
	return RateLimitedError{}, false
}
