package dnsimple

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
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
	domainsByName  map[string]fakeDomain
	nextDomainID   int64
	contacts       []fakeContact
	registrations  map[int64]*fakeRegistration
	nextRegID      int64
	certificates   map[string]map[int64]*fakeCertificate
	purchases      map[int64]string // purchase id -> domain
	nextCertID     int64
	charges        []fakeCharge
	remaining      int
	resetAt        time.Time
	forced429Until time.Time
	forced500Left  int
	apiCalls       int
}

type fakeDomain struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	AutoRenew bool   `json:"auto_renew"`
	ExpiresAt string `json:"expires_at"`
}

type fakeContact struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

// fakeCertificate advances toward "issued" one poll at a time, mirroring
// the registration state machine.
type fakeCertificate struct {
	ID         int64  `json:"id"`
	CommonName string `json:"common_name"`
	State      string `json:"state"`
	AutoRenew  bool   `json:"auto_renew"`
	ExpiresAt  string `json:"expires_at"`
	pollsLeft  int
}

type fakeCharge struct {
	InvoicedAt    string `json:"invoiced_at"`
	TotalAmount   string `json:"total_amount"`
	BalanceAmount string `json:"balance_amount"`
	Reference     string `json:"reference"`
	State         string `json:"state"`
}

// fakeRegistration advances toward "registered" one poll at a time, so
// workflow tests see the pending state before the terminal one.
type fakeRegistration struct {
	ID        int64  `json:"id"`
	State     string `json:"state"`
	domain    string
	pollsLeft int
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
		domainsByName: map[string]fakeDomain{},
		registrations: map[int64]*fakeRegistration{},
		certificates:  map[string]map[int64]*fakeCertificate{},
		purchases:     map[int64]string{},
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

func (fake *Fake) AddDomain(name, state string) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	fake.nextDomainID++
	fake.domainsByName[name] = fakeDomain{
		ID: fake.nextDomainID, Name: name, State: state,
		AutoRenew: false, ExpiresAt: "2027-07-22",
	}
}

func (fake *Fake) AddContact(firstName, lastName, email string) int64 {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	contactID := int64(len(fake.contacts) + 1)
	fake.contacts = append(fake.contacts, fakeContact{
		ID: contactID, FirstName: firstName, LastName: lastName, Email: email,
	})
	return contactID
}

func (fake *Fake) AddCharge(invoicedAt, total, reference, state string) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	fake.charges = append(fake.charges, fakeCharge{
		InvoicedAt: invoicedAt, TotalAmount: total, BalanceAmount: "0.00",
		Reference: reference, State: state,
	})
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

	fake.mux.HandleFunc("GET /{account}/domains", fake.api(fake.listDomains))
	fake.mux.HandleFunc("GET /{account}/contacts", fake.api(fake.listContacts))
	fake.mux.HandleFunc("GET /{account}/registrar/domains/{domain}/check", fake.api(fake.checkDomain))
	fake.mux.HandleFunc("POST /{account}/registrar/domains/{domain}/registrations", fake.api(fake.startRegistration))
	fake.mux.HandleFunc("GET /{account}/registrar/domains/{domain}/registrations/{registration}", fake.api(fake.pollRegistration))

	fake.mux.HandleFunc("GET /{account}/domains/{domain}/certificates", fake.api(fake.listCertificates))
	fake.mux.HandleFunc("POST /{account}/domains/{domain}/certificates/letsencrypt", fake.api(fake.purchaseLetsEncrypt))
	fake.mux.HandleFunc("POST /{account}/domains/{domain}/certificates/letsencrypt/{purchase}/issue", fake.api(fake.issueLetsEncrypt))
	fake.mux.HandleFunc("GET /{account}/domains/{domain}/certificates/{certificate}", fake.api(fake.pollCertificate))
	fake.mux.HandleFunc("GET /{account}/billing/charges", fake.api(fake.listCharges))
}

func (fake *Fake) listCertificates(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	var listed []*fakeCertificate
	for _, certificate := range fake.certificates[request.PathValue("domain")] {
		listed = append(listed, certificate)
	}
	sort.Slice(listed, func(left, right int) bool { return listed[left].ID < listed[right].ID })
	writePaged(response, request, listed)
}

func (fake *Fake) purchaseLetsEncrypt(response http.ResponseWriter, request *http.Request) {
	var payload struct {
		Name      string `json:"name"`
		AutoRenew bool   `json:"auto_renew"`
	}
	_ = json.NewDecoder(request.Body).Decode(&payload)
	fake.guard.Lock()
	defer fake.guard.Unlock()
	fake.nextCertID++
	commonName := request.PathValue("domain")
	if payload.Name != "" {
		commonName = payload.Name + "." + commonName
	}
	fake.purchases[fake.nextCertID] = commonName
	response.WriteHeader(http.StatusCreated)
	writeData(response, map[string]any{"id": fake.nextCertID, "state": "new"})
}

func (fake *Fake) issueLetsEncrypt(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	purchaseID, _ := strconv.ParseInt(request.PathValue("purchase"), 10, 64)
	commonName, exists := fake.purchases[purchaseID]
	domain := request.PathValue("domain")
	if !exists || !strings.HasSuffix(commonName, domain) {
		http.Error(response, `{"message":"purchase not found"}`, http.StatusNotFound)
		return
	}
	fake.nextCertID++
	certificate := &fakeCertificate{
		ID: fake.nextCertID, CommonName: commonName, State: "requesting",
		ExpiresAt: "2026-10-20", pollsLeft: 2,
	}
	if fake.certificates[domain] == nil {
		fake.certificates[domain] = map[int64]*fakeCertificate{}
	}
	fake.certificates[domain][certificate.ID] = certificate
	response.WriteHeader(http.StatusCreated)
	writeData(response, certificate)
}

func (fake *Fake) pollCertificate(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	certificateID, _ := strconv.ParseInt(request.PathValue("certificate"), 10, 64)
	certificate, exists := fake.certificates[request.PathValue("domain")][certificateID]
	if !exists {
		http.Error(response, `{"message":"certificate not found"}`, http.StatusNotFound)
		return
	}
	if certificate.pollsLeft > 0 {
		certificate.pollsLeft--
	}
	if certificate.pollsLeft == 0 && certificate.State != "issued" {
		certificate.State = "issued"
	}
	writeData(response, certificate)
}

func (fake *Fake) listCharges(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	writePaged(response, request, fake.charges)
}

func (fake *Fake) listDomains(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	var listed []fakeDomain
	for _, domain := range fake.domainsByName {
		listed = append(listed, domain)
	}
	sort.Slice(listed, func(left, right int) bool { return listed[left].Name < listed[right].Name })
	writePaged(response, request, listed)
}

func (fake *Fake) listContacts(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	writePaged(response, request, fake.contacts)
}

func (fake *Fake) checkDomain(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	domainName := request.PathValue("domain")
	_, taken := fake.domainsByName[domainName]
	writeData(response, map[string]any{"domain": domainName, "available": !taken, "premium": false})
}

func (fake *Fake) startRegistration(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	fake.nextRegID++
	registration := &fakeRegistration{
		ID: fake.nextRegID, State: "registering",
		domain: request.PathValue("domain"), pollsLeft: 2,
	}
	fake.registrations[registration.ID] = registration
	response.WriteHeader(http.StatusCreated)
	writeData(response, registration)
}

func (fake *Fake) pollRegistration(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	registrationID, _ := strconv.ParseInt(request.PathValue("registration"), 10, 64)
	registration, exists := fake.registrations[registrationID]
	if !exists {
		http.Error(response, `{"message":"registration not found"}`, http.StatusNotFound)
		return
	}
	if registration.pollsLeft > 0 {
		registration.pollsLeft--
	}
	if registration.pollsLeft == 0 && registration.State != "registered" {
		registration.State = "registered"
		fake.nextDomainID++
		fake.domainsByName[registration.domain] = fakeDomain{
			ID: fake.nextDomainID, Name: registration.domain,
			State: "registered", ExpiresAt: "2027-07-22",
		}
	}
	writeData(response, registration)
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

func (fake *Fake) listZones(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	type zoneJSON struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	var zoneNames []string
	for name := range fake.recordsByZone {
		zoneNames = append(zoneNames, name)
	}
	sort.Strings(zoneNames)
	var zones []zoneJSON
	for index, name := range zoneNames {
		zones = append(zones, zoneJSON{ID: int64(index + 1), Name: name})
	}
	writePaged(response, request, zones)
}

func (fake *Fake) listRecords(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	zone := request.PathValue("zone")
	var records []fakeRecord
	for _, record := range fake.recordsByZone[zone] {
		records = append(records, record)
	}
	sort.Slice(records, func(left, right int) bool { return records[left].ID < records[right].ID })
	writePaged(response, request, records)
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

// writePaged serves the slice the way DNSimple does: per_page/page from the
// query, plus the pagination envelope listAll walks. Items must arrive in a
// deterministic order or pages would overlap between calls.
func writePaged[Item any](response http.ResponseWriter, request *http.Request, items []Item) {
	page, _ := strconv.Atoi(request.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(request.URL.Query().Get("per_page"))
	if perPage < 1 {
		perPage = 30
	}
	totalPages := (len(items) + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	start := min((page-1)*perPage, len(items))
	end := min(start+perPage, len(items))
	_ = json.NewEncoder(response).Encode(map[string]any{
		"data": items[start:end],
		"pagination": map[string]int{
			"current_page": page, "per_page": perPage,
			"total_pages": totalPages, "total_entries": len(items),
		},
	})
}
