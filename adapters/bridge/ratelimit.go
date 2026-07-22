package bridge

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// attemptLimiter caps link-code attempts per client IP. The bridge is
// internet-exposed: codes are 40-bit, single-use, and short-lived, so brute
// force cannot win - but it should not be free to try either.
type attemptLimiter struct {
	guard        sync.Mutex
	attemptsByIP map[string][]time.Time
	limit        int
	window       time.Duration
	now          func() time.Time
}

func newAttemptLimiter(limit int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{
		attemptsByIP: map[string][]time.Time{},
		limit:        limit,
		window:       window,
		now:          time.Now,
	}
}

func (limiter *attemptLimiter) allow(clientIP string) bool {
	limiter.guard.Lock()
	defer limiter.guard.Unlock()
	horizon := limiter.now().Add(-limiter.window)
	recent := limiter.attemptsByIP[clientIP][:0]
	for _, attempt := range limiter.attemptsByIP[clientIP] {
		if attempt.After(horizon) {
			recent = append(recent, attempt)
		}
	}
	if len(recent) >= limiter.limit {
		limiter.attemptsByIP[clientIP] = recent
		return false
	}
	limiter.attemptsByIP[clientIP] = append(recent, limiter.now())
	return true
}

// clientIP trusts X-Forwarded-For because Caddy fronts the bridge and sets
// it; the raw RemoteAddr is Caddy itself.
func clientIP(request *http.Request) string {
	if forwarded := request.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}
