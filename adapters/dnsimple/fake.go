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

	guard             sync.Mutex
	recordsByZone     map[string]map[int64]fakeRecord
	nextRecordID      int64
	domainsByName     map[string]fakeDomain
	nextDomainID      int64
	contacts          []fakeContact
	registrations     map[int64]*fakeRegistration
	nextRegID         int64
	certificates      map[string]map[int64]*fakeCertificate
	purchases         map[int64]string // purchase id -> domain
	nextCertID        int64
	dnssecByDomain    map[string]bool
	forwards          map[string]map[int64]fakeForward
	nextForwardID     int64
	whoisByDomain     map[string]bool
	lockByDomain      map[string]bool
	delegation        map[string][]string
	nextRenewalID     int64
	availableServices []fakeService
	appliedServices   map[string][]int64 // domain -> service ids
	pushes            map[int64]fakePush
	nextPushID        int64
	registrantChanges map[int64]*fakeRegistrantChange
	zoneInactive      map[string]bool
	dsRecords         map[string]map[int64]fakeDSRecord
	nextDSRecordID    int64
	vanityByDomain    map[string]bool
	primaries         map[int64]*fakePrimary
	nextPrimaryID     int64
	secondaryZones    []string
	webhooks          map[int64]fakeWebhook
	nextWebhookID     int64
	nextChangeID      int64
	templates         map[int64]fakeTemplate
	templateRecs      map[int64]map[int64]fakeRecord
	nextTemplateID    int64
	charges           []fakeCharge
	remaining         int
	resetAt           time.Time
	forced429Until    time.Time
	forced500Left     int
	apiCalls          int
}

type fakeDomain struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	State        string `json:"state"`
	AutoRenew    bool   `json:"auto_renew"`
	PrivateWhois bool   `json:"private_whois"`
	ExpiresAt    string `json:"expires_at"`
}

func (fake *Fake) getDomain(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	domain, exists := fake.domainsByName[request.PathValue("domain")]
	if !exists {
		jsonError(response, `{"message":"domain not found"}`, http.StatusNotFound)
		return
	}
	writeData(response, domain)
}

type fakeContact struct {
	ID            int64  `json:"id"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	Address       string `json:"address1"`
	City          string `json:"city"`
	StateProvince string `json:"state_province"`
	PostalCode    string `json:"postal_code"`
	Country       string `json:"country"`
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

type fakeForward struct {
	ID               int64  `json:"id"`
	AliasEmail       string `json:"alias_email"`
	DestinationEmail string `json:"destination_email"`
}

type fakeService struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	SID         string `json:"sid"`
	Description string `json:"description"`
}

type fakePrimary struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	LinkedAt string `json:"linked_at"`
}

type fakeDSRecord struct {
	ID         int64  `json:"id"`
	KeyTag     string `json:"keytag"`
	Algorithm  string `json:"algorithm"`
	DigestType string `json:"digest_type"`
	Digest     string `json:"digest"`
}

type fakeWebhook struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
}

type fakeRegistrantChange struct {
	ID        int64  `json:"id"`
	State     string `json:"state"`
	DomainID  int64  `json:"domain_id"`
	ContactID int64  `json:"contact_id"`
	pollsLeft int
}

type fakePush struct {
	ID        int64  `json:"id"`
	DomainID  int64  `json:"domain_id"`
	CreatedAt string `json:"created_at"`
	domain    string
}

type fakeTemplate struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	SID         string `json:"sid"`
	Description string `json:"description"`
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
		mux:               http.NewServeMux(),
		account:           account,
		recordsByZone:     map[string]map[int64]fakeRecord{},
		domainsByName:     map[string]fakeDomain{},
		registrations:     map[int64]*fakeRegistration{},
		certificates:      map[string]map[int64]*fakeCertificate{},
		purchases:         map[int64]string{},
		dnssecByDomain:    map[string]bool{},
		forwards:          map[string]map[int64]fakeForward{},
		whoisByDomain:     map[string]bool{},
		lockByDomain:      map[string]bool{},
		delegation:        map[string][]string{},
		templates:         map[int64]fakeTemplate{},
		appliedServices:   map[string][]int64{},
		pushes:            map[int64]fakePush{},
		registrantChanges: map[int64]*fakeRegistrantChange{},
		zoneInactive:      map[string]bool{},
		dsRecords:         map[string]map[int64]fakeDSRecord{},
		vanityByDomain:    map[string]bool{},
		primaries:         map[int64]*fakePrimary{},
		webhooks:          map[int64]fakeWebhook{},
		availableServices: []fakeService{
			{ID: 1, Name: "GitHub Pages", SID: "github-pages", Description: "Static site hosting"},
			{ID: 2, Name: "Google Workspace", SID: "google-workspace", Description: "Mail and docs"},
		},
		templateRecs: map[int64]map[int64]fakeRecord{},
		remaining:    2400,
		resetAt:      time.Now().Add(time.Hour),
	}
	fake.routes()
	return fake
}

func (fake *Fake) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	// ServeMux cannot rank /tlds/{tld}/extended_attributes against
	// /{account}/domains/{domain} (neither is more specific), so the TLD
	// route dispatches before the mux.
	if request.Method == http.MethodGet {
		if tld, isAttributes := strings.CutSuffix(strings.TrimPrefix(request.URL.Path, "/tlds/"), "/extended_attributes"); isAttributes && !strings.Contains(tld, "/") && strings.HasPrefix(request.URL.Path, "/tlds/") {
			request.SetPathValue("tld", tld)
			fake.api(fake.listExtendedAttributes)(response, request)
			return
		}
	}
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
	fake.mux.HandleFunc("POST /{account}/contacts", fake.api(fake.createContact))
	fake.mux.HandleFunc("PATCH /{account}/contacts/{contact}", fake.api(fake.updateContact))
	fake.mux.HandleFunc("DELETE /{account}/contacts/{contact}", fake.api(fake.deleteContact))
	fake.mux.HandleFunc("GET /{account}/registrar/domains/{domain}/check", fake.api(fake.checkDomain))
	fake.mux.HandleFunc("POST /{account}/registrar/domains/{domain}/registrations", fake.api(fake.startRegistration))
	fake.mux.HandleFunc("GET /{account}/registrar/domains/{domain}/registrations/{registration}", fake.api(fake.pollRegistration))

	fake.mux.HandleFunc("GET /{account}/domains/{domain}/certificates", fake.api(fake.listCertificates))
	fake.mux.HandleFunc("POST /{account}/domains/{domain}/certificates/letsencrypt", fake.api(fake.purchaseLetsEncrypt))
	fake.mux.HandleFunc("POST /{account}/domains/{domain}/certificates/letsencrypt/{purchase}/issue", fake.api(fake.issueLetsEncrypt))
	fake.mux.HandleFunc("GET /{account}/domains/{domain}/certificates/{certificate}", fake.api(fake.pollCertificate))
	fake.mux.HandleFunc("GET /{account}/billing/charges", fake.api(fake.listCharges))

	fake.mux.HandleFunc("GET /{account}/domains/{domain}/dnssec", fake.api(fake.getDNSSEC))
	fake.mux.HandleFunc("POST /{account}/domains/{domain}/dnssec", fake.api(fake.setDNSSEC(true)))
	fake.mux.HandleFunc("DELETE /{account}/domains/{domain}/dnssec", fake.api(fake.setDNSSEC(false)))
	fake.mux.HandleFunc("GET /{account}/domains/{domain}/email_forwards", fake.api(fake.listForwards))
	fake.mux.HandleFunc("POST /{account}/domains/{domain}/email_forwards", fake.api(fake.createForward))
	fake.mux.HandleFunc("DELETE /{account}/domains/{domain}/email_forwards/{forward}", fake.api(fake.deleteForward))
	fake.mux.HandleFunc("PUT /{account}/registrar/domains/{domain}/auto_renewal", fake.api(fake.setAutoRenewal(true)))
	fake.mux.HandleFunc("DELETE /{account}/registrar/domains/{domain}/auto_renewal", fake.api(fake.setAutoRenewal(false)))

	fake.mux.HandleFunc("GET /{account}/domains/{domain}", fake.api(fake.getDomain))
	fake.mux.HandleFunc("PUT /{account}/registrar/domains/{domain}/whois_privacy", fake.api(fake.flagWrite(func(fake *Fake) map[string]bool { return fake.whoisByDomain }, true, http.StatusOK)))
	fake.mux.HandleFunc("DELETE /{account}/registrar/domains/{domain}/whois_privacy", fake.api(fake.flagWrite(func(fake *Fake) map[string]bool { return fake.whoisByDomain }, false, http.StatusOK)))
	fake.mux.HandleFunc("GET /{account}/registrar/domains/{domain}/transfer_lock", fake.api(fake.flagRead(func(fake *Fake) map[string]bool { return fake.lockByDomain })))
	fake.mux.HandleFunc("POST /{account}/registrar/domains/{domain}/transfer_lock", fake.api(fake.flagWrite(func(fake *Fake) map[string]bool { return fake.lockByDomain }, true, http.StatusCreated)))
	fake.mux.HandleFunc("DELETE /{account}/registrar/domains/{domain}/transfer_lock", fake.api(fake.flagWrite(func(fake *Fake) map[string]bool { return fake.lockByDomain }, false, http.StatusOK)))
	fake.mux.HandleFunc("GET /{account}/registrar/domains/{domain}/delegation", fake.api(fake.getDelegation))
	fake.mux.HandleFunc("PUT /{account}/registrar/domains/{domain}/delegation", fake.api(fake.putDelegation))
	fake.mux.HandleFunc("POST /{account}/registrar/domains/{domain}/renewals", fake.api(fake.startRenewal))
	fake.mux.HandleFunc("POST /{account}/registrar/domains/{domain}/transfers", fake.api(fake.startTransfer))
	fake.mux.HandleFunc("GET /{account}/registrar/domains/{domain}/transfers/{transfer}", fake.api(fake.pollTransfer))
	fake.mux.HandleFunc("POST /{account}/registrar/domains/{domain}/authorize_transfer_out", fake.api(fake.authorizeTransferOut))
	fake.mux.HandleFunc("POST /{account}/registrar/domains/{domain}/restores", fake.api(fake.startRestore))

	fake.mux.HandleFunc("GET /{account}/templates", fake.api(fake.listTemplates))
	fake.mux.HandleFunc("POST /{account}/templates", fake.api(fake.createTemplate))
	fake.mux.HandleFunc("DELETE /{account}/templates/{template}", fake.api(fake.deleteTemplate))
	fake.mux.HandleFunc("GET /{account}/templates/{template}/records", fake.api(fake.listTemplateRecords))
	fake.mux.HandleFunc("POST /{account}/templates/{template}/records", fake.api(fake.createTemplateRecord))
	fake.mux.HandleFunc("DELETE /{account}/templates/{template}/records/{record}", fake.api(fake.deleteTemplateRecord))
	fake.mux.HandleFunc("POST /{account}/domains/{domain}/templates/{template}", fake.api(fake.applyTemplate))

	fake.mux.HandleFunc("GET /services", fake.api(fake.listAvailableServices))
	fake.mux.HandleFunc("GET /{account}/domains/{domain}/services", fake.api(fake.listAppliedServices))
	fake.mux.HandleFunc("POST /{account}/domains/{domain}/services/{service}", fake.api(fake.applyService))
	fake.mux.HandleFunc("DELETE /{account}/domains/{domain}/services/{service}", fake.api(fake.unapplyService))
	fake.mux.HandleFunc("POST /{account}/domains/{domain}/pushes", fake.api(fake.initiatePush))
	fake.mux.HandleFunc("GET /{account}/pushes", fake.api(fake.listPushes))
	fake.mux.HandleFunc("POST /{account}/pushes/{push}", fake.api(fake.acceptPush))
	fake.mux.HandleFunc("DELETE /{account}/pushes/{push}", fake.api(fake.rejectPush))
	fake.mux.HandleFunc("POST /{account}/registrar/registrant_changes/check", fake.api(fake.checkRegistrantChange))
	fake.mux.HandleFunc("POST /{account}/registrar/registrant_changes", fake.api(fake.createRegistrantChange))
	fake.mux.HandleFunc("GET /{account}/registrar/registrant_changes/{change}", fake.api(fake.pollRegistrantChange))
	fake.mux.HandleFunc("GET /{account}/registrar/registrant_changes", fake.api(fake.listRegistrantChanges))
	fake.mux.HandleFunc("DELETE /{account}/registrar/registrant_changes/{change}", fake.api(fake.cancelRegistrantChange))
	fake.mux.HandleFunc("DELETE /{account}/registrar/domains/{domain}/transfers/{transfer}", fake.api(fake.cancelTransfer))
	fake.mux.HandleFunc("PUT /{account}/zones/{zone}/activation", fake.api(fake.setZoneActivation(true)))
	fake.mux.HandleFunc("DELETE /{account}/zones/{zone}/activation", fake.api(fake.setZoneActivation(false)))
	fake.mux.HandleFunc("GET /{account}/zones/{zone}/distribution", fake.api(fake.zoneDistribution))
	fake.mux.HandleFunc("GET /{account}/webhooks", fake.api(fake.listWebhooks))
	fake.mux.HandleFunc("POST /{account}/webhooks", fake.api(fake.createWebhook))
	fake.mux.HandleFunc("DELETE /{account}/webhooks/{webhook}", fake.api(fake.deleteWebhook))
	fake.mux.HandleFunc("GET /{account}/secondary_dns/primaries", fake.api(fake.listPrimaries))
	fake.mux.HandleFunc("POST /{account}/secondary_dns/primaries", fake.api(fake.createPrimary))
	fake.mux.HandleFunc("DELETE /{account}/secondary_dns/primaries/{primary}", fake.api(fake.deletePrimary))
	fake.mux.HandleFunc("PUT /{account}/secondary_dns/primaries/{primary}/link", fake.api(fake.setPrimaryLink(true)))
	fake.mux.HandleFunc("PUT /{account}/secondary_dns/primaries/{primary}/unlink", fake.api(fake.setPrimaryLink(false)))
	fake.mux.HandleFunc("POST /{account}/secondary_dns/zones", fake.api(fake.createSecondaryZone))
	fake.mux.HandleFunc("GET /{account}/dns_analytics", fake.api(fake.dnsAnalytics))
	fake.mux.HandleFunc("GET /tlds", fake.api(fake.listTLDs))
	fake.mux.HandleFunc("GET /{account}/registrar/domains/{domain}/prices", fake.api(fake.domainPrices))
	fake.mux.HandleFunc("GET /{account}/domains/{domain}/certificates/{certificate}/download", fake.api(fake.downloadCertificate))
	fake.mux.HandleFunc("GET /{account}/domains/{domain}/certificates/{certificate}/private_key", fake.api(fake.certificatePrivateKey))
	fake.mux.HandleFunc("GET /{account}/zones/{zone}/file", fake.api(fake.zoneFile))
	fake.mux.HandleFunc("GET /{account}/domains/{domain}/ds_records", fake.api(fake.listDSRecords))
	fake.mux.HandleFunc("POST /{account}/domains/{domain}/ds_records", fake.api(fake.createDSRecord))
	fake.mux.HandleFunc("DELETE /{account}/domains/{domain}/ds_records/{ds}", fake.api(fake.deleteDSRecord))
	fake.mux.HandleFunc("PATCH /{account}/templates/{template}", fake.api(fake.updateTemplate))
	fake.mux.HandleFunc("GET /{account}/zones/{zone}/records/{record}/distribution", fake.api(fake.zoneDistribution))
	fake.mux.HandleFunc("PUT /{account}/vanity/{domain}", fake.api(fake.setVanity(true)))
	fake.mux.HandleFunc("DELETE /{account}/vanity/{domain}", fake.api(fake.setVanity(false)))
}

func (fake *Fake) startTransfer(response http.ResponseWriter, request *http.Request) {
	var payload struct {
		AuthCode string `json:"auth_code"`
	}
	_ = json.NewDecoder(request.Body).Decode(&payload)
	if payload.AuthCode == "" {
		jsonError(response, `{"message":"auth code required"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	fake.nextRegID++
	// Transfers reuse the registration state machine shape: pending until
	// the second poll, then terminal.
	fake.registrations[fake.nextRegID] = &fakeRegistration{
		ID: fake.nextRegID, State: "transferring",
		domain: request.PathValue("domain"), pollsLeft: 2,
	}
	response.WriteHeader(http.StatusCreated)
	writeData(response, map[string]any{"id": fake.nextRegID, "state": "transferring"})
}

func (fake *Fake) pollTransfer(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	transferID, _ := strconv.ParseInt(request.PathValue("transfer"), 10, 64)
	transfer, exists := fake.registrations[transferID]
	if !exists {
		jsonError(response, `{"message":"transfer not found"}`, http.StatusNotFound)
		return
	}
	if transfer.pollsLeft > 0 {
		transfer.pollsLeft--
	}
	// A cancelled transfer is terminal; production never revives it.
	if transfer.pollsLeft == 0 && transfer.State != "transferred" && transfer.State != "cancelled" {
		transfer.State = "transferred"
		fake.nextDomainID++
		fake.domainsByName[transfer.domain] = fakeDomain{
			ID: fake.nextDomainID, Name: transfer.domain,
			State: "registered", ExpiresAt: "2027-07-22",
		}
	}
	writeData(response, transfer)
}

func (fake *Fake) authorizeTransferOut(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	if _, exists := fake.domainsByName[request.PathValue("domain")]; !exists {
		jsonError(response, `{"message":"domain not found"}`, http.StatusNotFound)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) startRestore(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	if _, exists := fake.domainsByName[request.PathValue("domain")]; !exists {
		jsonError(response, `{"message":"domain not found"}`, http.StatusNotFound)
		return
	}
	fake.nextRenewalID++
	response.WriteHeader(http.StatusCreated)
	writeData(response, map[string]any{"id": fake.nextRenewalID, "state": "new"})
}

func (fake *Fake) flagRead(flags func(*Fake) map[string]bool) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		fake.guard.Lock()
		defer fake.guard.Unlock()
		// Mirror production: a feature never touched answers 404, not
		// enabled:false - the gateway translates it.
		enabled, touched := flags(fake)[request.PathValue("domain")]
		if !touched {
			jsonError(response, `{"message":"WHOIS privacy not found"}`, http.StatusNotFound)
			return
		}
		writeData(response, map[string]bool{"enabled": enabled})
	}
}

func (fake *Fake) flagWrite(flags func(*Fake) map[string]bool, enabled bool, status int) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		fake.guard.Lock()
		defer fake.guard.Unlock()
		domainName := request.PathValue("domain")
		flags(fake)[domainName] = enabled
		var domainID int64
		if domain, exists := fake.domainsByName[domainName]; exists {
			domain.PrivateWhois = fake.whoisByDomain[domainName]
			fake.domainsByName[domainName] = domain
			domainID = domain.ID
		}
		response.WriteHeader(status)
		if status != http.StatusNoContent {
			writeData(response, map[string]any{"id": domainID, "domain_id": domainID, "enabled": enabled})
		}
	}
}

func (fake *Fake) getDelegation(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	nameServers, configured := fake.delegation[request.PathValue("domain")]
	if !configured {
		nameServers = []string{"ns1.dnsimple.com", "ns2.dnsimple.com", "ns3.dnsimple.com", "ns4.dnsimple.com"}
	}
	writeData(response, nameServers)
}

func (fake *Fake) putDelegation(response http.ResponseWriter, request *http.Request) {
	var nameServers []string
	if err := json.NewDecoder(request.Body).Decode(&nameServers); err != nil {
		jsonError(response, `{"message":"bad payload"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	fake.delegation[request.PathValue("domain")] = nameServers
	writeData(response, nameServers)
}

func (fake *Fake) startRenewal(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	if _, exists := fake.domainsByName[request.PathValue("domain")]; !exists {
		jsonError(response, `{"message":"domain not found"}`, http.StatusNotFound)
		return
	}
	fake.nextRenewalID++
	response.WriteHeader(http.StatusCreated)
	writeData(response, map[string]any{"id": fake.nextRenewalID, "state": "new"})
}

func (fake *Fake) getDNSSEC(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	writeData(response, map[string]bool{"enabled": fake.dnssecByDomain[request.PathValue("domain")]})
}

func (fake *Fake) setDNSSEC(enabled bool) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		fake.guard.Lock()
		defer fake.guard.Unlock()
		fake.dnssecByDomain[request.PathValue("domain")] = enabled
		if enabled {
			response.WriteHeader(http.StatusCreated)
			writeData(response, map[string]bool{"enabled": true})
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func (fake *Fake) listForwards(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	var listed []fakeForward
	for _, forward := range fake.forwards[request.PathValue("domain")] {
		listed = append(listed, forward)
	}
	sort.Slice(listed, func(left, right int) bool { return listed[left].ID < listed[right].ID })
	writePaged(response, request, listed)
}

func (fake *Fake) createForward(response http.ResponseWriter, request *http.Request) {
	var payload struct {
		AliasName        string `json:"alias_name"`
		DestinationEmail string `json:"destination_email"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		jsonError(response, `{"message":"bad payload"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	domain := request.PathValue("domain")
	if fake.forwards[domain] == nil {
		fake.forwards[domain] = map[int64]fakeForward{}
	}
	fake.nextForwardID++
	forward := fakeForward{
		ID:               fake.nextForwardID,
		AliasEmail:       payload.AliasName + "@" + domain,
		DestinationEmail: payload.DestinationEmail,
	}
	fake.forwards[domain][forward.ID] = forward
	response.WriteHeader(http.StatusCreated)
	writeData(response, forward)
}

func (fake *Fake) deleteForward(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	domain := request.PathValue("domain")
	forwardID, _ := strconv.ParseInt(request.PathValue("forward"), 10, 64)
	if _, exists := fake.forwards[domain][forwardID]; !exists {
		jsonError(response, `{"message":"email forward not found"}`, http.StatusNotFound)
		return
	}
	delete(fake.forwards[domain], forwardID)
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) setAutoRenewal(enabled bool) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		fake.guard.Lock()
		defer fake.guard.Unlock()
		domain, exists := fake.domainsByName[request.PathValue("domain")]
		if !exists {
			jsonError(response, `{"message":"domain not found"}`, http.StatusNotFound)
			return
		}
		domain.AutoRenew = enabled
		fake.domainsByName[request.PathValue("domain")] = domain
		response.WriteHeader(http.StatusNoContent)
	}
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
		jsonError(response, `{"message":"purchase not found"}`, http.StatusNotFound)
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
	response.WriteHeader(http.StatusAccepted)
	writeData(response, certificate)
}

func (fake *Fake) pollCertificate(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	certificateID, _ := strconv.ParseInt(request.PathValue("certificate"), 10, 64)
	certificate, exists := fake.certificates[request.PathValue("domain")][certificateID]
	if !exists {
		jsonError(response, `{"message":"certificate not found"}`, http.StatusNotFound)
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
		jsonError(response, `{"message":"registration not found"}`, http.StatusNotFound)
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
			jsonError(response, `{"message":"internal server error"}`, http.StatusInternalServerError)
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
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Secondary bool   `json:"secondary"`
		Active    bool   `json:"active"`
	}
	secondary := map[string]bool{}
	for _, name := range fake.secondaryZones {
		secondary[name] = true
	}
	zoneNames := map[string]bool{}
	for name := range fake.recordsByZone {
		zoneNames[name] = true
	}
	for name := range secondary {
		zoneNames[name] = true
	}
	var ordered []string
	for name := range zoneNames {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	var zones []zoneJSON
	for index, name := range ordered {
		zones = append(zones, zoneJSON{ID: int64(index + 1), Name: name, Secondary: secondary[name], Active: !fake.zoneInactive[name]})
		_ = index
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
		jsonError(response, `{"message":"bad payload"}`, http.StatusBadRequest)
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
		jsonError(response, `{"message":"bad payload"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	zone := request.PathValue("zone")
	recordID, _ := strconv.ParseInt(request.PathValue("record"), 10, 64)
	record, exists := fake.recordsByZone[zone][recordID]
	if !exists {
		jsonError(response, `{"message":"record not found"}`, http.StatusNotFound)
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
		jsonError(response, `{"message":"record not found"}`, http.StatusNotFound)
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
		jsonError(response, `{"message":"bad payload"}`, http.StatusBadRequest)
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
	if change.Creates == nil {
		change.Creates = []fakeRecord{}
	}
	if change.Updates == nil {
		change.Updates = []fakeRecord{}
	}
	deleted := []map[string]int{}
	deleted = append(deleted, change.Deletes...)
	writeData(response, map[string]any{"creates": change.Creates, "updates": change.Updates, "deletes": deleted})
}

// jsonError mirrors production's error envelope: same JSON body shape,
// application/json content type (http.Error would stamp text/plain).
func jsonError(response http.ResponseWriter, body string, status int) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, _ = response.Write([]byte(body))
}

func writeData(response http.ResponseWriter, data any) {
	_ = json.NewEncoder(response).Encode(map[string]any{"data": data})
}

// writePaged serves the slice the way DNSimple does: per_page/page from the
// query, plus the pagination envelope listAll walks. Items must arrive in a
// deterministic order or pages would overlap between calls.
func writePaged[Item any](response http.ResponseWriter, request *http.Request, items []Item) {
	if items == nil {
		items = []Item{}
	}
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

func (fake *Fake) createContact(response http.ResponseWriter, request *http.Request) {
	var draft fakeContact
	if err := json.NewDecoder(request.Body).Decode(&draft); err != nil {
		jsonError(response, `{"message":"bad payload"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	draft.ID = int64(len(fake.contacts) + 1)
	fake.contacts = append(fake.contacts, draft)
	response.WriteHeader(http.StatusCreated)
	writeData(response, draft)
}

func (fake *Fake) updateContact(response http.ResponseWriter, request *http.Request) {
	var patch fakeContact
	if err := json.NewDecoder(request.Body).Decode(&patch); err != nil {
		jsonError(response, `{"message":"bad payload"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	contactID, _ := strconv.ParseInt(request.PathValue("contact"), 10, 64)
	for index, contact := range fake.contacts {
		if contact.ID == contactID {
			patch.ID = contactID
			fake.contacts[index] = patch
			writeData(response, patch)
			return
		}
	}
	jsonError(response, `{"message":"contact not found"}`, http.StatusNotFound)
}

func (fake *Fake) deleteContact(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	contactID, _ := strconv.ParseInt(request.PathValue("contact"), 10, 64)
	for index, contact := range fake.contacts {
		if contact.ID == contactID {
			fake.contacts = append(fake.contacts[:index], fake.contacts[index+1:]...)
			response.WriteHeader(http.StatusNoContent)
			return
		}
	}
	jsonError(response, `{"message":"contact not found"}`, http.StatusNotFound)
}

func (fake *Fake) listTemplates(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	var listed []fakeTemplate
	for _, template := range fake.templates {
		listed = append(listed, template)
	}
	sort.Slice(listed, func(left, right int) bool { return listed[left].ID < listed[right].ID })
	writePaged(response, request, listed)
}

func (fake *Fake) createTemplate(response http.ResponseWriter, request *http.Request) {
	var draft fakeTemplate
	if err := json.NewDecoder(request.Body).Decode(&draft); err != nil {
		jsonError(response, `{"message":"bad payload"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	fake.nextTemplateID++
	draft.ID = fake.nextTemplateID
	fake.templates[draft.ID] = draft
	fake.templateRecs[draft.ID] = map[int64]fakeRecord{}
	response.WriteHeader(http.StatusCreated)
	writeData(response, draft)
}

func (fake *Fake) deleteTemplate(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	templateID, _ := strconv.ParseInt(request.PathValue("template"), 10, 64)
	if _, exists := fake.templates[templateID]; !exists {
		jsonError(response, `{"message":"template not found"}`, http.StatusNotFound)
		return
	}
	delete(fake.templates, templateID)
	delete(fake.templateRecs, templateID)
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) listTemplateRecords(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	templateID, _ := strconv.ParseInt(request.PathValue("template"), 10, 64)
	var listed []fakeRecord
	for _, record := range fake.templateRecs[templateID] {
		listed = append(listed, record)
	}
	sort.Slice(listed, func(left, right int) bool { return listed[left].ID < listed[right].ID })
	writePaged(response, request, listed)
}

func (fake *Fake) createTemplateRecord(response http.ResponseWriter, request *http.Request) {
	var draft fakeRecord
	if err := json.NewDecoder(request.Body).Decode(&draft); err != nil {
		jsonError(response, `{"message":"bad payload"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	templateID, _ := strconv.ParseInt(request.PathValue("template"), 10, 64)
	if _, exists := fake.templates[templateID]; !exists {
		jsonError(response, `{"message":"template not found"}`, http.StatusNotFound)
		return
	}
	fake.nextRecordID++
	draft.ID = fake.nextRecordID
	fake.templateRecs[templateID][draft.ID] = draft
	response.WriteHeader(http.StatusCreated)
	writeData(response, draft)
}

func (fake *Fake) deleteTemplateRecord(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	templateID, _ := strconv.ParseInt(request.PathValue("template"), 10, 64)
	recordID, _ := strconv.ParseInt(request.PathValue("record"), 10, 64)
	if _, exists := fake.templateRecs[templateID][recordID]; !exists {
		jsonError(response, `{"message":"record not found"}`, http.StatusNotFound)
		return
	}
	delete(fake.templateRecs[templateID], recordID)
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) applyTemplate(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	templateID, _ := strconv.ParseInt(request.PathValue("template"), 10, 64)
	domain := request.PathValue("domain")
	if _, exists := fake.templates[templateID]; !exists {
		jsonError(response, `{"message":"template not found"}`, http.StatusNotFound)
		return
	}
	if fake.recordsByZone[domain] == nil {
		fake.recordsByZone[domain] = map[int64]fakeRecord{}
	}
	for _, record := range fake.templateRecs[templateID] {
		fake.nextRecordID++
		record.ID = fake.nextRecordID
		record.ZoneID = domain
		fake.recordsByZone[domain][record.ID] = record
	}
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) listAvailableServices(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	writePaged(response, request, fake.availableServices)
}

func (fake *Fake) listAppliedServices(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	var applied []fakeService
	for _, serviceID := range fake.appliedServices[request.PathValue("domain")] {
		for _, service := range fake.availableServices {
			if service.ID == serviceID {
				applied = append(applied, service)
			}
		}
	}
	writePaged(response, request, applied)
}

func (fake *Fake) applyService(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	domain := request.PathValue("domain")
	serviceID, _ := strconv.ParseInt(request.PathValue("service"), 10, 64)
	fake.appliedServices[domain] = append(fake.appliedServices[domain], serviceID)
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) unapplyService(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	domain := request.PathValue("domain")
	serviceID, _ := strconv.ParseInt(request.PathValue("service"), 10, 64)
	var remaining []int64
	for _, applied := range fake.appliedServices[domain] {
		if applied != serviceID {
			remaining = append(remaining, applied)
		}
	}
	fake.appliedServices[domain] = remaining
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) initiatePush(response http.ResponseWriter, request *http.Request) {
	var payload struct {
		NewAccountEmail string `json:"new_account_email"`
	}
	_ = json.NewDecoder(request.Body).Decode(&payload)
	if payload.NewAccountEmail == "" {
		jsonError(response, `{"message":"new_account_email required"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	domain, exists := fake.domainsByName[request.PathValue("domain")]
	if !exists {
		jsonError(response, `{"message":"domain not found"}`, http.StatusNotFound)
		return
	}
	fake.nextPushID++
	push := fakePush{ID: fake.nextPushID, DomainID: domain.ID, CreatedAt: "2026-07-22T12:00:00Z", domain: domain.Name}
	fake.pushes[push.ID] = push
	response.WriteHeader(http.StatusCreated)
	writeData(response, push)
}

func (fake *Fake) listPushes(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	var listed []fakePush
	for _, push := range fake.pushes {
		listed = append(listed, push)
	}
	sort.Slice(listed, func(left, right int) bool { return listed[left].ID < listed[right].ID })
	writePaged(response, request, listed)
}

func (fake *Fake) acceptPush(response http.ResponseWriter, request *http.Request) {
	var payload struct {
		ContactID int64 `json:"contact_id"`
	}
	_ = json.NewDecoder(request.Body).Decode(&payload)
	if payload.ContactID == 0 {
		jsonError(response, `{"message":"contact_id required"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	pushID, _ := strconv.ParseInt(request.PathValue("push"), 10, 64)
	if _, exists := fake.pushes[pushID]; !exists {
		jsonError(response, `{"message":"push not found"}`, http.StatusNotFound)
		return
	}
	delete(fake.pushes, pushID)
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) rejectPush(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	pushID, _ := strconv.ParseInt(request.PathValue("push"), 10, 64)
	if _, exists := fake.pushes[pushID]; !exists {
		jsonError(response, `{"message":"push not found"}`, http.StatusNotFound)
		return
	}
	delete(fake.pushes, pushID)
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) checkRegistrantChange(response http.ResponseWriter, _ *http.Request) {
	writeData(response, map[string]any{"extended_attributes": []string{}, "registry_owner_change": true})
}

func (fake *Fake) createRegistrantChange(response http.ResponseWriter, request *http.Request) {
	var payload struct {
		DomainID  int64 `json:"domain_id"`
		ContactID int64 `json:"contact_id"`
	}
	_ = json.NewDecoder(request.Body).Decode(&payload)
	fake.guard.Lock()
	defer fake.guard.Unlock()
	fake.nextChangeID++
	change := &fakeRegistrantChange{
		ID: fake.nextChangeID, State: "pending",
		DomainID: payload.DomainID, ContactID: payload.ContactID, pollsLeft: 2,
	}
	fake.registrantChanges[change.ID] = change
	response.WriteHeader(http.StatusCreated)
	writeData(response, change)
}

func (fake *Fake) pollRegistrantChange(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	changeID, _ := strconv.ParseInt(request.PathValue("change"), 10, 64)
	change, exists := fake.registrantChanges[changeID]
	if !exists {
		jsonError(response, `{"message":"registrant change not found"}`, http.StatusNotFound)
		return
	}
	if change.pollsLeft > 0 {
		change.pollsLeft--
	}
	// Cancelled changes are terminal; production never revives them.
	if change.pollsLeft == 0 && change.State != "completed" && change.State != "cancelled" {
		change.State = "completed"
	}
	writeData(response, change)
}

func (fake *Fake) setZoneActivation(active bool) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		fake.guard.Lock()
		defer fake.guard.Unlock()
		fake.zoneInactive[request.PathValue("zone")] = !active
		writeData(response, map[string]any{"name": request.PathValue("zone"), "active": active})
	}
}

func (fake *Fake) zoneDistribution(response http.ResponseWriter, _ *http.Request) {
	writeData(response, map[string]bool{"distributed": true})
}

func (fake *Fake) listWebhooks(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	var listed []fakeWebhook
	for _, webhook := range fake.webhooks {
		listed = append(listed, webhook)
	}
	sort.Slice(listed, func(left, right int) bool { return listed[left].ID < listed[right].ID })
	writePaged(response, request, listed)
}

func (fake *Fake) createWebhook(response http.ResponseWriter, request *http.Request) {
	var payload fakeWebhook
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.URL == "" {
		jsonError(response, `{"message":"url required"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	fake.nextWebhookID++
	payload.ID = fake.nextWebhookID
	fake.webhooks[payload.ID] = payload
	response.WriteHeader(http.StatusCreated)
	writeData(response, payload)
}

func (fake *Fake) deleteWebhook(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	webhookID, _ := strconv.ParseInt(request.PathValue("webhook"), 10, 64)
	if _, exists := fake.webhooks[webhookID]; !exists {
		jsonError(response, `{"message":"webhook not found"}`, http.StatusNotFound)
		return
	}
	delete(fake.webhooks, webhookID)
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) listPrimaries(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	var listed []*fakePrimary
	for _, primary := range fake.primaries {
		listed = append(listed, primary)
	}
	sort.Slice(listed, func(left, right int) bool { return listed[left].ID < listed[right].ID })
	writePaged(response, request, listed)
}

func (fake *Fake) createPrimary(response http.ResponseWriter, request *http.Request) {
	var payload fakePrimary
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.IP == "" {
		jsonError(response, `{"message":"ip required"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	fake.nextPrimaryID++
	payload.ID = fake.nextPrimaryID
	fake.primaries[payload.ID] = &payload
	response.WriteHeader(http.StatusCreated)
	writeData(response, payload)
}

func (fake *Fake) deletePrimary(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	primaryID, _ := strconv.ParseInt(request.PathValue("primary"), 10, 64)
	if _, exists := fake.primaries[primaryID]; !exists {
		jsonError(response, `{"message":"primary not found"}`, http.StatusNotFound)
		return
	}
	delete(fake.primaries, primaryID)
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) setPrimaryLink(linked bool) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		fake.guard.Lock()
		defer fake.guard.Unlock()
		primaryID, _ := strconv.ParseInt(request.PathValue("primary"), 10, 64)
		primary, exists := fake.primaries[primaryID]
		if !exists {
			jsonError(response, `{"message":"primary not found"}`, http.StatusNotFound)
			return
		}
		primary.LinkedAt = ""
		if linked {
			primary.LinkedAt = "2026-07-23T12:00:00Z"
		}
		writeData(response, primary)
	}
}

func (fake *Fake) createSecondaryZone(response http.ResponseWriter, request *http.Request) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.Name == "" {
		jsonError(response, `{"message":"name required"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	fake.secondaryZones = append(fake.secondaryZones, payload.Name)
	response.WriteHeader(http.StatusCreated)
	writeData(response, payload)
}

// dnsAnalytics mirrors the real endpoint's envelope: data is an OBJECT of
// headers plus rows of values, unlike every other list. The first fake
// served array-of-objects and hid a production decode failure - the fake
// must drift toward production, never toward the gateway's assumptions.
func (fake *Fake) dnsAnalytics(response http.ResponseWriter, _ *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	var zoneNames []string
	for name := range fake.recordsByZone {
		zoneNames = append(zoneNames, name)
	}
	sort.Strings(zoneNames)
	rows := make([][]any, 0, len(zoneNames))
	for index, name := range zoneNames {
		rows = append(rows, []any{name, 1000 * (index + 1)})
	}
	_ = json.NewEncoder(response).Encode(map[string]any{
		"data": map[string]any{
			"headers": []string{"zone_name", "volume"},
			"rows":    rows,
		},
		"pagination": map[string]int{
			"current_page": 1, "per_page": 100, "total_pages": 1, "total_entries": len(rows),
		},
	})
}

// listExtendedAttributes mirrors the spec's uk example for .uk and the
// empty list production sends for TLDs without registry extras.
func (fake *Fake) listExtendedAttributes(response http.ResponseWriter, request *http.Request) {
	type optionJSON struct {
		Title       string `json:"title"`
		Value       string `json:"value"`
		Description string `json:"description"`
	}
	type attributeJSON struct {
		Name        string       `json:"name"`
		Title       string       `json:"title"`
		Description string       `json:"description"`
		Required    bool         `json:"required"`
		Options     []optionJSON `json:"options"`
	}
	if request.PathValue("tld") != "uk" {
		writeData(response, []attributeJSON{})
		return
	}
	writeData(response, []attributeJSON{{
		Name: "uk_legal_type", Title: "Legal type", Description: "Legal type of registrant contact", Required: true,
		Options: []optionJSON{
			{Title: "UK Individual", Value: "IND", Description: "UK Individual (our default value)"},
			{Title: "UK Limited Company", Value: "LTD", Description: "UK Limited Company"},
		},
	}})
}

func (fake *Fake) listTLDs(response http.ResponseWriter, request *http.Request) {
	type tldJSON struct {
		TLD                 string `json:"tld"`
		RegistrationEnabled bool   `json:"registration_enabled"`
		TransferEnabled     bool   `json:"transfer_enabled"`
		WhoisPrivacy        bool   `json:"whois_privacy"`
		IDN                 bool   `json:"idn"`
		MinimumRegistration int    `json:"minimum_registration"`
	}
	writePaged(response, request, []tldJSON{
		{TLD: "com", RegistrationEnabled: true, TransferEnabled: true, WhoisPrivacy: true, IDN: true, MinimumRegistration: 1},
		{TLD: "dev", RegistrationEnabled: true, TransferEnabled: true, WhoisPrivacy: false, IDN: false, MinimumRegistration: 1},
	})
}

func (fake *Fake) domainPrices(response http.ResponseWriter, request *http.Request) {
	writeData(response, map[string]any{
		"domain": request.PathValue("domain"), "premium": false,
		"registration_price": 14.5, "renewal_price": 14.5, "transfer_price": 14.5,
	})
}

func (fake *Fake) issuedCertificate(request *http.Request) (*fakeCertificate, bool) {
	certificateID, _ := strconv.ParseInt(request.PathValue("certificate"), 10, 64)
	certificate, exists := fake.certificates[request.PathValue("domain")][certificateID]
	if !exists || certificate.State != "issued" {
		return nil, false
	}
	return certificate, true
}

func (fake *Fake) downloadCertificate(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	certificate, ready := fake.issuedCertificate(request)
	if !ready {
		jsonError(response, `{"message":"certificate not issued"}`, http.StatusNotFound)
		return
	}
	writeData(response, map[string]any{
		"server": "-----BEGIN CERTIFICATE-----\nserver-" + certificate.CommonName + "\n-----END CERTIFICATE-----",
		"chain":  []string{"-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----"},
	})
}

func (fake *Fake) certificatePrivateKey(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	if _, ready := fake.issuedCertificate(request); !ready {
		jsonError(response, `{"message":"certificate not issued"}`, http.StatusNotFound)
		return
	}
	writeData(response, map[string]string{
		"private_key": "-----BEGIN PRIVATE KEY-----\nfake-key\n-----END PRIVATE KEY-----",
	})
}

func (fake *Fake) zoneFile(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	zone := request.PathValue("zone")
	file := "$ORIGIN " + zone + ".\n$TTL 3600\n"
	var ids []int64
	for id := range fake.recordsByZone[zone] {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	for _, id := range ids {
		record := fake.recordsByZone[zone][id]
		name := record.Name
		if name == "" {
			name = "@"
		}
		file += fmt.Sprintf("%s %d IN %s %s\n", name, record.TTL, record.Type, record.Content)
	}
	writeData(response, map[string]string{"zone": file})
}

func (fake *Fake) listDSRecords(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	var listed []fakeDSRecord
	for _, record := range fake.dsRecords[request.PathValue("domain")] {
		listed = append(listed, record)
	}
	sort.Slice(listed, func(left, right int) bool { return listed[left].ID < listed[right].ID })
	writePaged(response, request, listed)
}

func (fake *Fake) createDSRecord(response http.ResponseWriter, request *http.Request) {
	var draft fakeDSRecord
	if err := json.NewDecoder(request.Body).Decode(&draft); err != nil || draft.Digest == "" {
		jsonError(response, `{"message":"digest required"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	domain := request.PathValue("domain")
	if fake.dsRecords[domain] == nil {
		fake.dsRecords[domain] = map[int64]fakeDSRecord{}
	}
	fake.nextDSRecordID++
	draft.ID = fake.nextDSRecordID
	fake.dsRecords[domain][draft.ID] = draft
	response.WriteHeader(http.StatusCreated)
	writeData(response, draft)
}

func (fake *Fake) deleteDSRecord(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	domain := request.PathValue("domain")
	dsRecordID, _ := strconv.ParseInt(request.PathValue("ds"), 10, 64)
	if _, exists := fake.dsRecords[domain][dsRecordID]; !exists {
		jsonError(response, `{"message":"ds record not found"}`, http.StatusNotFound)
		return
	}
	delete(fake.dsRecords[domain], dsRecordID)
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) updateTemplate(response http.ResponseWriter, request *http.Request) {
	var patch fakeTemplate
	if err := json.NewDecoder(request.Body).Decode(&patch); err != nil {
		jsonError(response, `{"message":"bad payload"}`, http.StatusBadRequest)
		return
	}
	fake.guard.Lock()
	defer fake.guard.Unlock()
	templateID, _ := strconv.ParseInt(request.PathValue("template"), 10, 64)
	existing, found := fake.templates[templateID]
	if !found {
		jsonError(response, `{"message":"template not found"}`, http.StatusNotFound)
		return
	}
	existing.Name, existing.SID, existing.Description = patch.Name, patch.SID, patch.Description
	fake.templates[templateID] = existing
	writeData(response, existing)
}

func (fake *Fake) setVanity(enabled bool) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		fake.guard.Lock()
		defer fake.guard.Unlock()
		if _, exists := fake.domainsByName[request.PathValue("domain")]; !exists {
			jsonError(response, `{"message":"domain not found"}`, http.StatusNotFound)
			return
		}
		fake.vanityByDomain[request.PathValue("domain")] = enabled
		if enabled {
			writeData(response, []map[string]any{{"id": 1, "name": "ns1." + request.PathValue("domain")}})
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func (fake *Fake) listRegistrantChanges(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	var listed []*fakeRegistrantChange
	for _, change := range fake.registrantChanges {
		listed = append(listed, change)
	}
	sort.Slice(listed, func(left, right int) bool { return listed[left].ID < listed[right].ID })
	writePaged(response, request, listed)
}

func (fake *Fake) cancelRegistrantChange(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	changeID, _ := strconv.ParseInt(request.PathValue("change"), 10, 64)
	change, exists := fake.registrantChanges[changeID]
	if !exists {
		jsonError(response, `{"message":"registrant change not found"}`, http.StatusNotFound)
		return
	}
	if change.State == "completed" || change.State == "cancelled" {
		jsonError(response, `{"message":"the registrant change is already final"}`, http.StatusBadRequest)
		return
	}
	change.State = "cancelled"
	change.pollsLeft = 0
	response.WriteHeader(http.StatusNoContent)
}

func (fake *Fake) cancelTransfer(response http.ResponseWriter, request *http.Request) {
	fake.guard.Lock()
	defer fake.guard.Unlock()
	transferID, _ := strconv.ParseInt(request.PathValue("transfer"), 10, 64)
	transfer, exists := fake.registrations[transferID]
	if !exists {
		jsonError(response, `{"message":"transfer not found"}`, http.StatusNotFound)
		return
	}
	transfer.State = "cancelled"
	transfer.pollsLeft = 0
	response.WriteHeader(http.StatusAccepted)
	writeData(response, transfer)
}
