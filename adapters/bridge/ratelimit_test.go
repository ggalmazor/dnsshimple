package bridge

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLinkAttemptsAreCappedPerClientIP(test *testing.T) {
	// Given a bridge with a tight limiter (no Component needed: the limiter
	// answers before the code is ever looked up)
	bridge := LinkBridge{Attempts: newAttemptLimiter(3, time.Minute)}
	router := http.NewServeMux()
	router.HandleFunc("POST /link", bridge.limited(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNotFound) // stand-in for the real handler
	}))
	server := httptest.NewServer(router)
	test.Cleanup(server.Close)

	// When one IP hammers and another asks once
	attempt := func(ip string) int {
		request, _ := http.NewRequest(http.MethodPost, server.URL+"/link",
			nil)
		request.Header.Set("X-Forwarded-For", ip)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Body = http.NoBody
		response, err := server.Client().Do(request)
		if err != nil {
			test.Fatalf("attempt: %v", err)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	for range 3 {
		if status := attempt("203.0.113.7"); status != http.StatusNotFound {
			test.Fatalf("within the limit must pass through, got %d", status)
		}
	}

	// Then the fourth from that IP is refused, while the other IP passes
	if status := attempt("203.0.113.7"); status != http.StatusTooManyRequests {
		test.Fatalf("expected 429 over the limit, got %d", status)
	}
	if status := attempt("198.51.100.9"); status != http.StatusNotFound {
		test.Fatalf("another IP must be unaffected, got %d", status)
	}
}
