package dnsimple

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"dnsshimple/core/gateway"
	"dnsshimple/core/telemetry"
)

func testClient(test *testing.T) (*Client, *Fake) {
	test.Helper()
	fake := NewFake(FakeAccount{ID: 2416, Email: "guille@sendia.org"})
	server := httptest.NewServer(fake)
	test.Cleanup(server.Close)
	return NewClient(server.URL, server.Client(), telemetry.NewMetrics()), fake
}

func TestReadsAreCachedWithinTheTTLAndInvalidatedByWrites(test *testing.T) {
	// Given a zone behind the client
	client, fake := testClient(test)
	fake.AddZone("example.com", Seed("www", "A", "1.2.3.4", 3600))
	ctx := context.Background()

	// When the same read runs twice, then a write, then the read again
	first := client.Get(ctx, "token", "/2416/zones/example.com/records", "/zones/records")
	second := client.Get(ctx, "token", "/2416/zones/example.com/records", "/zones/records")
	written := client.Write(ctx, "token", "POST", "/2416/zones/example.com/records", "/zones/records",
		map[string]any{"name": "api", "type": "A", "content": "5.6.7.8", "ttl": 3600})
	third := client.Get(ctx, "token", "/2416/zones/example.com/records", "/zones/records")

	// Then nothing failed, the second read cost no API call, and the write
	// invalidated the cache so the third read went upstream again
	for name, outcome := range map[string]error{
		"first": first.Error(), "second": second.Error(), "write": written.Error(), "third": third.Error(),
	} {
		if outcome != nil {
			test.Fatalf("%s failed: %v", name, outcome)
		}
	}
	if calls := fake.APICalls(); calls != 3 { // read, write, read - not 4
		test.Fatalf("expected 3 upstream calls (cached second read), got %d", calls)
	}
}

func TestConcurrentIdenticalReadsShareOneFlight(test *testing.T) {
	// Given a cold cache and 8 goroutines asking the same question
	client, fake := testClient(test)
	fake.AddZone("example.com", Seed("www", "A", "1.2.3.4", 3600))

	var waitGroup sync.WaitGroup
	for range 8 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_ = client.Get(context.Background(), "token", "/2416/zones", "/zones")
		}()
	}
	waitGroup.Wait()

	// Then at most one flight went upstream (allowing a scheduling straggler)
	if calls := fake.APICalls(); calls > 2 {
		test.Fatalf("expected singleflighted reads, got %d upstream calls", calls)
	}
}

func TestA429SurfacesTypedAndPreemptsTheNextCall(test *testing.T) {
	// Given a DNSimple whose quota is exhausted until a known reset
	client, fake := testClient(test)
	resumeAt := time.Now().Add(30 * time.Minute).Truncate(time.Second)
	fake.Force429(resumeAt)
	ctx := context.Background()

	// When a read hits the wall and another follows
	first := client.Get(ctx, "token", "/2416/zones", "/zones")
	callsAfterFirst := fake.APICalls()
	second := client.Get(ctx, "token", "/2416/zones", "/zones")

	// Then both fail with the typed error carrying DNSimple's reset time,
	// and the second never reached the API
	for name, outcome := range map[string]error{"first": first.Error(), "second": second.Error()} {
		rateLimited, isRateLimited := gateway.IsRateLimited(outcome)
		if !isRateLimited {
			test.Fatalf("%s: expected RateLimitedError, got %v", name, outcome)
		}
		if !rateLimited.ResumeAt.Equal(resumeAt) {
			test.Fatalf("%s: ResumeAt %v, expected %v", name, rateLimited.ResumeAt, resumeAt)
		}
	}
	if fake.APICalls() != callsAfterFirst {
		test.Fatal("the second call was not preempted client-side")
	}
}

func TestRejectionsCarryDNSimplesOwnMessage(test *testing.T) {
	// Given a DNSimple that rejects with its usual {"message": ...} body
	rejecting := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(response, `{"message":"TLD .term is invalid or not supported"}`)
	}))
	test.Cleanup(rejecting.Close)
	client := NewClient(rejecting.URL, rejecting.Client(), telemetry.NewMetrics())

	// When a call is rejected
	outcome := client.Get(context.Background(), "token", "/2416/registrar/domains/x.term/check", "/registrar/check")

	// Then the typed failure carries the upstream message verbatim
	var rejection gateway.UpstreamRejectionError
	if !errors.As(outcome.Error(), &rejection) {
		test.Fatalf("expected UpstreamRejectionError, got %v", outcome.Error())
	}
	if rejection.StatusCode != 400 || rejection.Message != "TLD .term is invalid or not supported" {
		test.Fatalf("rejection lost the message: %+v", rejection)
	}
}

func TestSlowAnswersSurfaceAsTypedTimeouts(test *testing.T) {
	// Given a DNSimple slower than the per-call deadline
	slow := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-request.Context().Done():
		}
		response.WriteHeader(http.StatusOK)
	}))
	test.Cleanup(slow.Close)
	client := NewClient(slow.URL, slow.Client(), telemetry.NewMetrics())
	client.callTimeout = 50 * time.Millisecond

	// When a call outlives its deadline
	outcome := client.Get(context.Background(), "token", "/2416/zones", "/zones")

	// Then the failure is the typed timeout naming the operation
	var timedOut gateway.UpstreamTimeoutError
	if !errors.As(outcome.Error(), &timedOut) || timedOut.Operation != "/zones" {
		test.Fatalf("expected UpstreamTimeoutError for /zones, got %v", outcome.Error())
	}
}

func TestRegistrationWritesGetTheSlowDeadline(test *testing.T) {
	client := NewClient("http://unused", nil, telemetry.NewMetrics())

	if deadline := client.timeoutFor("POST", "/registrar/registrations"); deadline != 90*time.Second {
		test.Fatalf("registration writes: %v", deadline)
	}
	if deadline := client.timeoutFor("GET", "/registrar/registrations"); deadline != 15*time.Second {
		test.Fatalf("registration polls must stay fast: %v", deadline)
	}
	if deadline := client.timeoutFor("POST", "/zones/records"); deadline != 15*time.Second {
		test.Fatalf("ordinary writes: %v", deadline)
	}
}

func TestRepeatedServerErrorsOpenTheCircuit(test *testing.T) {
	// Given a DNSimple failing hard
	client, fake := testClient(test)
	fake.Force500(3)
	ctx := context.Background()

	// When three calls fail and a fourth follows
	for range 3 {
		if outcome := client.Get(ctx, "token", "/2416/zones", "/zones"); !outcome.Failed() {
			test.Fatal("a 500 must fail the call")
		}
		// Each attempt must be a fresh upstream call: cold cache per failure.
		client.guard.Lock()
		delete(client.cache, "token /2416/zones")
		client.guard.Unlock()
	}
	callsBefore := fake.APICalls()
	fourth := client.Get(ctx, "token", "/2416/zones", "/zones")

	// Then the fourth fails fast on the open circuit, upstream untouched
	if !errors.Is(fourth.Error(), gateway.ErrUpstreamDown) {
		test.Fatalf("expected ErrUpstreamDown, got %v", fourth.Error())
	}
	if fake.APICalls() != callsBefore {
		test.Fatal("the open circuit still let a call through")
	}
}
