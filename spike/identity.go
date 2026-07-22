package main

import (
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"time"
)

// LinkedAccount is a DNSimple account bound to an SSH public key.
type LinkedAccount struct {
	AccountID   int64
	Email       string
	AccessToken string
}

// LinkRequest is one pending browser handshake, created when an unknown SSH
// key connects and resolved when the OAuth callback completes.
type LinkRequest struct {
	Code        string
	Fingerprint string
	StartedAt   time.Time
	Completed   chan LinkedAccount
}

const (
	linkCodeAlphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789" // no 0/O, 1/I/L lookalikes
	linkCodeLength   = 8
	linkCodeLifetime = 10 * time.Minute
)

// IdentityStore is the spike's in-memory stand-in for the identity component:
// pending link codes on one side, fingerprint-to-account bindings on the other.
// Production replaces this with the Postgres-backed identity component.
type IdentityStore struct {
	guard                sync.Mutex
	pendingByCode        map[string]*LinkRequest
	accountByFingerprint map[string]LinkedAccount
}

func NewIdentityStore() *IdentityStore {
	return &IdentityStore{
		pendingByCode:        make(map[string]*LinkRequest),
		accountByFingerprint: make(map[string]LinkedAccount),
	}
}

// AccountFor answers whether this SSH fingerprint is already linked.
func (store *IdentityStore) AccountFor(fingerprint string) (LinkedAccount, bool) {
	store.guard.Lock()
	defer store.guard.Unlock()
	account, known := store.accountByFingerprint[fingerprint]
	return account, known
}

// BeginLink issues a fresh single-use code for an unknown fingerprint.
func (store *IdentityStore) BeginLink(fingerprint string) *LinkRequest {
	request := &LinkRequest{
		Code:        newLinkCode(),
		Fingerprint: fingerprint,
		StartedAt:   time.Now(),
		Completed:   make(chan LinkedAccount, 1),
	}
	store.guard.Lock()
	defer store.guard.Unlock()
	store.pendingByCode[request.Code] = request
	return request
}

// PendingLink finds a live (non-expired) link request by user-typed code.
func (store *IdentityStore) PendingLink(rawCode string) (*LinkRequest, bool) {
	code := NormalizeLinkCode(rawCode)
	store.guard.Lock()
	defer store.guard.Unlock()
	request, found := store.pendingByCode[code]
	if !found {
		return nil, false
	}
	if time.Since(request.StartedAt) > linkCodeLifetime {
		delete(store.pendingByCode, code)
		return nil, false
	}
	return request, true
}

// CompleteLink binds the account to the requesting fingerprint, consumes the
// code, and wakes the SSH session waiting on the request channel.
func (store *IdentityStore) CompleteLink(rawCode string, account LinkedAccount) error {
	code := NormalizeLinkCode(rawCode)
	store.guard.Lock()
	request, found := store.pendingByCode[code]
	if !found {
		store.guard.Unlock()
		return fmt.Errorf("link code %q is unknown or already used", code)
	}
	delete(store.pendingByCode, code)
	store.accountByFingerprint[request.Fingerprint] = account
	store.guard.Unlock()

	request.Completed <- account // buffered(1): never blocks
	return nil
}

// NormalizeLinkCode uppercases and strips separators, so "kz3q-7hwp" and
// "KZ3Q 7HWP" both match the stored "KZ3Q7HWP".
func NormalizeLinkCode(raw string) string {
	cleaned := strings.ToUpper(raw)
	cleaned = strings.Map(func(character rune) rune {
		if strings.ContainsRune(linkCodeAlphabet, character) {
			return character
		}
		return -1
	}, cleaned)
	return cleaned
}

// DisplayLinkCode renders "KZ3Q7HWP" as the friendlier "KZ3Q-7HWP".
func DisplayLinkCode(code string) string {
	if len(code) != linkCodeLength {
		return code
	}
	return code[:4] + "-" + code[4:]
}

func newLinkCode() string {
	randomBytes := make([]byte, linkCodeLength)
	if _, err := rand.Read(randomBytes); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err)) // spike-only: no entropy means nothing works
	}
	var code strings.Builder
	for _, randomByte := range randomBytes {
		code.WriteByte(linkCodeAlphabet[int(randomByte)%len(linkCodeAlphabet)])
	}
	return code.String()
}
