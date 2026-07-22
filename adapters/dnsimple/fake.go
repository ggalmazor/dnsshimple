package dnsimple

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// FakeAccount is what Fake asserts about every redeemed token.
type FakeAccount struct {
	ID    int64
	Email string
}

// Fake stands in for DNSimple in grey-box tests: the OAuth handshake, the
// zones API with stateful records, and the rate-limit headers every real
// response carries. Helpers let tests force 429s and 5xx to exercise the
// gateway's protection stack.
type Fake struct {
	mux     *http.ServeMux
	account FakeAccount

	guard          sync.Mutex
	recordsByZone  map[string]map[int64]fakeRecord
	nextRecordID   int64
	remaining      int
	resetAt        time.Time
	forced429Until time.Time
	forced500Left  int
	apiCalls       int
}

type fakeRecord struct {
	ID       int64  `json:"id"`
	ZoneID   string `json:"zone_id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Priority *int   `json:"priority"`
}

func NewFake(account FakeAccount) *Fake {
	fake := &Fake{
		mux:           http.NewServeMux(),
		account:       account,
		recordsByZone: map[string]map[int64]fakeRecord{},
		remaining:     2400,
		resetAt:       time.Now().Add(time.Hour),
	}
	fake.routes()
	return fake
}

func (fake *Fake) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	fake.mux.ServeHTTP(response, request)
}

// --- test helpers ---------------------------------------------------------

// AddZone seeds a zone with records and returns their assigned ids.
func (fake *Fake) AddZone(zoneName string, records ...fakeSeed) []int64 {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	if fake.recordsByZone[zoneName] == nil {
		fake.recordsByZone[zoneName] = map[int64]fakeRecord{}
	}
	var assigned []int64
	for _, seed := range records {
		fake.nextRecordID++
		fake.recordsByZone[zoneName][fake.nextRecordID] = fakeRecord{
			ID: fake.nextRecordID, ZoneID: zoneName,
			Name: seed.Name, Type: seed.Type, Content: seed.Content, TTL: seed.TTL,
		}
		assigned = append(assigned, fake.nextRecordID)
	}
	return assigned
}

type fakeSeed struct {
	Name, Type, Content string
	TTL                 int
}

func Seed(name, recordType, content string, ttl int) fakeSeed {
	return fakeSeed{Name: name, Type: recordType, Content: content, TTL: ttl}
}

// Force429 makes every API call answer 429 until the given time, which also
// rides the X-RateLimit-Reset header.
func (fake *Fake) Force429(until time.Time) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	fake.forced429Until = until
	fake.resetAt = until
}

// Force500 makes the next n API calls answer 500.
func (fake *Fake) Force500(n int) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	fake.forced500Left = n
}

// APICalls counts the authenticated API requests that reached the fake -
// the number the cache and the breaker are supposed to keep down.
func (fake *Fake) APICalls() int {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	return fake.apiCalls
}

// --- routes ---------------------------------------------------------------

func (fake *Fake) routes() {
	// OAuth: the consent screen collapsed to an instant yes.
	fake.mux.HandleFunc("GET /oauth/authorize", func(response http.ResponseWriter, request *http.Request) {
		redirectTarget, err := url.Parse(request.URL.Query().Get("redirect_uri"))
		if err != nil || redirectTarget.String() == "" {
			http.Error(response, "missing redirect_uri", http.StatusBadRequest)
			return
		}
		bounceQuery := redirectTarget.Query()
		bounceQuery.Set("code", "fake-grant-code")
		bounceQuery.Set("state", request.URL.Query().Get("state"))
		redirectTarget.RawQuery = bounceQuery.Encode()
		http.Redirect(response, request, redirectTarget.String(), http.StatusFound)
	})
	fake.mux.HandleFunc("POST /oauth/access_token", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(response, `{"access_token":"fake-access-token-%d","token_type":"Bearer"}`, fake.account.ID)
	})
	fake.mux.HandleFunc("GET /whoami", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(response,
			`{"data":{"user":null,"account":{"id":%d,"email":%q}}}`, fake.account.ID, fake.account.Email)
	})

	fake.mux.HandleFunc("GET /{account}/zones", fake.api(fake.listZones))
	fake.mux.HandleFunc("GET /{account}/zones/{zone}/records", fake.api(fake.listRecords))
	fake.mux.HandleFunc("POST /{account}/zones/{zone}/records", fake.api(fake.createRecord))
	fake.mux.HandleFunc("PATCH /{account}/zones/{zone}/records/{record}", fake.api(fake.updateRecord))
	fake.mux.HandleFunc("DELETE /{account}/zones/{zone}/records/{record}", fake.api(fake.deleteRecord))
	fake.mux.HandleFunc("POST /{account}/zones/{zone}/batch", fake.api(fake.batchChange))
}

// api wraps every authenticated endpoint with the behaviors a real DNSimple
// exhibits: rate-limit headers on everything, 429 when exhausted or forced,
// and forced 5xx for breaker tests.
func (fake *Fake) api(handle http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		fake.guard.Lock()
		fake.apiCalls++
		if fake.forced500Left > 0 {
			fake.forced500Left--
			fake.guard.Unlock()
			http.Error(response, `{"message":"internal server error"}`, http.StatusInternalServerError)
			return
		}
		if time.Now().Before(fake.forced429Until) || fake.remaining <= 0 {
			response.Header().Set("X-RateLimit-Limit", "2400")
			response.Header().Set("X-RateLimit-Remaining", "0")
			response.Header().Set("X-RateLimit-Reset", strconv.FormatInt(fake.resetAt.Unix(), 10))
			fake.guard.Unlock()
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(response, `{"message":"quota exceeded"}`)
			return
		}
		fake.remaining--
		response.Header().Set("X-RateLimit-Limit", "2400")
		response.Header().Set("X-RateLimit-Remaining", strconv.Itoa(fake.remaining))
		response.Header().Set("X-RateLimit-Reset", strconv.FormatInt(fake.resetAt.Unix(), 10))
		fake.guard.Unlock()

		response.Header().Set("Content-Type", "application/json")
		handle(response, request)
	}
}

func (fake *Fake) listZones(response http.ResponseWriter, _ *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	type zoneJSON struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	var zones []zoneJSON
	index := int64(0)
	for name := range fake.recordsByZone {
		index++
		zones = append(zones, zoneJSON{ID: index, Name: name})
	}
	writeData(response, zones)
}

func (fake *Fake) listRecords(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	zone := request.PathValue("zone")
	var records []fakeRecord
	for _, record := range fake.recordsByZone[zone] {
		records = append(records, record)
	}
	writeData(response, records)
}

func (fake *Fake) createRecord(response http.ResponseWriter, request *http.Request) {
	var draft fakeRecord
	if err := json.NewDecoder(request.Body).Decode(&draft); err != nil {
		http.Error(response, `{"message":"bad payload"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	zone := request.PathValue("zone")
	if fake.recordsByZone[zone] == nil {
		fake.recordsByZone[zone] = map[int64]fakeRecord{}
	}
	fake.nextRecordID++
	draft.ID = fake.nextRecordID
	draft.ZoneID = zone
	fake.recordsByZone[zone][draft.ID] = draft
	response.WriteHeader(http.StatusCreated)
	writeData(response, draft)
}

func (fake *Fake) updateRecord(response http.ResponseWriter, request *http.Request) {
	var patch fakeRecord
	if err := json.NewDecoder(request.Body).Decode(&patch); err != nil {
		http.Error(response, `{"message":"bad payload"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	zone := request.PathValue("zone")
	recordID, _ := strconv.ParseInt(request.PathValue("record"), 10, 64)
	record, exists := fake.recordsByZone[zone][recordID]
	if !exists {
		http.Error(response, `{"message":"record not found"}`, http.StatusNotFound)
		return
	}
	record.Name, record.Type, record.Content, record.TTL, record.Priority =
		patch.Name, patch.Type, patch.Content, patch.TTL, patch.Priority
	fake.recordsByZone[zone][recordID] = record
	writeData(response, record)
}

func (fake *Fake) deleteRecord(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	zone := request.PathValue("zone")
	recordID, _ := strconv.ParseInt(request.PathValue("record"), 10, 64)
	if _, exists := fake.recordsByZone[zone][recordID]; !exists {
		http.Error(response, `{"message":"record not found"}`, http.StatusNotFound)
		return
	}
	delete(fake.recordsByZone[zone], recordID)
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) batchChange(response http.ResponseWriter, request *http.Request) {
	var change struct {
		Creates []fakeRecord     `json:"creates"`
		Updates []fakeRecord     `json:"updates"`
		Deletes []map[string]int `json:"deletes"`
	}
	if err := json.NewDecoder(request.Body).Decode(&change); err != nil {
		http.Error(response, `{"message":"bad payload"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	zone := request.PathValue("zone")
	if fake.recordsByZone[zone] == nil {
		fake.recordsByZone[zone] = map[int64]fakeRecord{}
	}
	for _, draft := range change.Creates {
		fake.nextRecordID++
		draft.ID = fake.nextRecordID
		draft.ZoneID = zone
		fake.recordsByZone[zone][draft.ID] = draft
	}
	for _, patch := range change.Updates {
		if record, exists := fake.recordsByZone[zone][patch.ID]; exists {
			record.Name, record.Type, record.Content, record.TTL =
				patch.Name, patch.Type, patch.Content, patch.TTL
			fake.recordsByZone[zone][record.ID] = record
		}
	}
	for _, deletion := range change.Deletes {
		delete(fake.recordsByZone[zone], int64(deletion["id"]))
	}
	response.WriteHeader(http.StatusAccepted)
	writeData(response, map[string]string{"status": "accepted"})
}

func writeData(response http.ResponseWriter, data any) {
	_ = json.NewEncoder(response).Encode(map[string]any{"data": data})
}
