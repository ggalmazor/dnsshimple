package dnsimple

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"dnsshimple/core/gateway"
	"dnsshimple/core/telemetry"
	"dnsshimple/kit"
)

// Client is the rate-limit-aware door to the DNSimple API (DESIGN.md §5).
// Every API call of every Component goes through it, so the budget tracking,
// the read cache, the single-flighting, and the circuit breaker exist exactly
// once. Endpoint labels are caller-provided constants, never raw paths:
// paths carry account ids and zone names, and metrics labels must not.
type Client struct {
	apiBaseURL string
	web        *http.Client
	metrics    *telemetry.Metrics
	flights    singleflight.Group
	now        func() time.Time

	guard   sync.Mutex
	cache   map[string]cachedRead // token+path -> body
	budgets map[string]rateBudget // token -> remaining/reset
	breaker breakerState
}

type cachedRead struct {
	body      []byte
	expiresAt time.Time
}

type rateBudget struct {
	remaining int
	resetAt   time.Time
}

type breakerState struct {
	consecutiveFailures int
	openUntil           time.Time
}

const (
	readCacheTTL     = 45 * time.Second
	breakerThreshold = 3
	breakerCooldown  = 30 * time.Second
)

func NewClient(apiBaseURL string, web *http.Client, metrics *telemetry.Metrics) *Client {
	return &Client{
		apiBaseURL: apiBaseURL,
		web:        web,
		metrics:    metrics,
		now:        time.Now,
		cache:      map[string]cachedRead{},
		budgets:    map[string]rateBudget{},
	}
}

// Get reads JSON with the full protection stack: cache, preemptive budget
// check, breaker, and single-flighting of identical concurrent reads.
func (client *Client) Get(ctx context.Context, accessToken, path, endpointLabel string) kit.Outcome[[]byte] {
	cacheKey := accessToken + " " + path

	client.guard.Lock()
	if cached, found := client.cache[cacheKey]; found && client.now().Before(cached.expiresAt) {
		client.guard.Unlock()
		client.metrics.CacheOutcomes.WithLabelValues("hit").Inc()
		return kit.Success(cached.body)
	}
	client.guard.Unlock()
	client.metrics.CacheOutcomes.WithLabelValues("miss").Inc()

	if refusal := client.refuseWithoutCalling(accessToken); refusal != nil {
		return kit.Failure[[]byte](refusal)
	}

	body, err, _ := client.flights.Do(cacheKey, func() (any, error) {
		return client.call(ctx, accessToken, http.MethodGet, path, endpointLabel, nil)
	})
	if err != nil {
		return kit.Failure[[]byte](err)
	}

	client.guard.Lock()
	client.cache[cacheKey] = cachedRead{body: body.([]byte), expiresAt: client.now().Add(readCacheTTL)}
	client.guard.Unlock()
	return kit.Success(body.([]byte))
}

// Write sends a mutation and drops the tenant's read cache: the cheap,
// always-correct invalidation for a UI that re-reads what it just changed.
func (client *Client) Write(ctx context.Context, accessToken, method, path, endpointLabel string, payload any) kit.Outcome[[]byte] {
	if refusal := client.refuseWithoutCalling(accessToken); refusal != nil {
		return kit.Failure[[]byte](refusal)
	}

	var requestBody []byte
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return kit.Failure[[]byte](fmt.Errorf("encoding the %s payload: %w", endpointLabel, err))
		}
		requestBody = encoded
	}

	responseBody, err := client.call(ctx, accessToken, method, path, endpointLabel, requestBody)
	if err != nil {
		return kit.Failure[[]byte](err)
	}

	client.guard.Lock()
	for key := range client.cache {
		if strings.HasPrefix(key, accessToken+" ") {
			delete(client.cache, key)
		}
	}
	client.guard.Unlock()
	return kit.Success(responseBody)
}

// refuseWithoutCalling is the preemptive layer: an exhausted budget or an
// open circuit fails fast without spending a request.
func (client *Client) refuseWithoutCalling(accessToken string) error {
	client.guard.Lock()
	defer client.guard.Unlock()
	if budget, tracked := client.budgets[accessToken]; tracked &&
		budget.remaining <= 0 && client.now().Before(budget.resetAt) {
		return gateway.RateLimitedError{ResumeAt: budget.resetAt}
	}
	if client.breaker.consecutiveFailures >= breakerThreshold &&
		client.now().Before(client.breaker.openUntil) {
		return gateway.ErrUpstreamDown
	}
	return nil
}

func (client *Client) call(ctx context.Context, accessToken, method, path, endpointLabel string, requestBody []byte) ([]byte, error) {
	var bodyReader io.Reader
	if requestBody != nil {
		bodyReader = bytes.NewReader(requestBody)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.apiBaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("building the %s request: %w", endpointLabel, err)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := client.web.Do(request)
	if err != nil {
		client.recordFailure()
		return nil, fmt.Errorf("calling DNSimple %s: %w", endpointLabel, err)
	}
	defer response.Body.Close()
	client.metrics.DNSimpleCalls.WithLabelValues(endpointLabel, strconv.Itoa(response.StatusCode)).Inc()
	client.trackBudget(accessToken, response)

	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		client.metrics.RateLimited.Inc()
		return nil, gateway.RateLimitedError{ResumeAt: client.resetFrom(response)}
	case response.StatusCode >= 500:
		client.recordFailure()
		return nil, fmt.Errorf("DNSimple %s answered %s", endpointLabel, response.Status)
	case response.StatusCode >= 400:
		client.recordSuccess() // the upstream is healthy; the request was wrong
		message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return nil, fmt.Errorf("DNSimple %s rejected the request (%s): %s",
			endpointLabel, response.Status, strings.TrimSpace(string(message)))
	}

	client.recordSuccess()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("reading the %s response: %w", endpointLabel, err)
	}
	return responseBody, nil
}

// trackBudget seeds the per-token budget from response headers - the numbers
// come from DNSimple itself, never from client-side guessing.
func (client *Client) trackBudget(accessToken string, response *http.Response) {
	remaining, err := strconv.Atoi(response.Header.Get("X-RateLimit-Remaining"))
	if err != nil {
		return
	}
	client.metrics.RateLimitRemaining.Set(float64(remaining))
	client.guard.Lock()
	client.budgets[accessToken] = rateBudget{remaining: remaining, resetAt: client.resetFrom(response)}
	client.guard.Unlock()
}

func (client *Client) resetFrom(response *http.Response) time.Time {
	if unixSeconds, err := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		return time.Unix(unixSeconds, 0)
	}
	// No usable header: back off a full window rather than retrying blind.
	return client.now().Add(time.Hour)
}

func (client *Client) recordFailure() {
	client.guard.Lock()
	defer client.guard.Unlock()
	client.breaker.consecutiveFailures++
	if client.breaker.consecutiveFailures >= breakerThreshold {
		client.breaker.openUntil = client.now().Add(breakerCooldown)
	}
}

func (client *Client) recordSuccess() {
	client.guard.Lock()
	defer client.guard.Unlock()
	client.breaker = breakerState{}
}
