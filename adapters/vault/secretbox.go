// Package vault is the driven adapter that seals DNSimple access tokens at
// rest with NaCl secretbox (DESIGN.md §6).
package vault

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/nacl/secretbox"

	"dnsshimple/kit"
)

// ErrCannotOpen deliberately carries no detail: a failed open means a wrong
// key or tampered ciphertext, and neither deserves a differentiating message.
var ErrCannotOpen = errors.New("vault: cannot open the sealed token")

const nonceLength = 24

// Secretbox seals with a single 32-byte key taken from the environment/KMS.
type Secretbox struct {
	key [32]byte
}

// FromBase64Key builds the vault from the standard-base64 32-byte key in
// DNSSHIMPLE_VAULT_KEY. Generate one with NewRandomBase64Key.
func FromBase64Key(encoded string) (Secretbox, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Secretbox{}, fmt.Errorf("vault key is not valid base64: %w", err)
	}
	if len(raw) != 32 {
		return Secretbox{}, fmt.Errorf("vault key must be 32 bytes, got %d", len(raw))
	}
	var vault Secretbox
	copy(vault.key[:], raw)
	return vault, nil
}

func NewRandomBase64Key() string {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	return base64.StdEncoding.EncodeToString(key[:])
}

// Seal encrypts the token as nonce||box.
func (vault Secretbox) Seal(accessToken string) kit.Outcome[[]byte] {
	var nonce [nonceLength]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return kit.Failure[[]byte](fmt.Errorf("drawing a nonce: %w", err))
	}
	sealed := secretbox.Seal(nonce[:], []byte(accessToken), &nonce, &vault.key)
	return kit.Success(sealed)
}

func (vault Secretbox) Open(sealedToken []byte) kit.Outcome[string] {
	if len(sealedToken) <= nonceLength {
		return kit.Failure[string](ErrCannotOpen)
	}
	var nonce [nonceLength]byte
	copy(nonce[:], sealedToken[:nonceLength])
	opened, authentic := secretbox.Open(nil, sealedToken[nonceLength:], &nonce, &vault.key)
	if !authentic {
		return kit.Failure[string](ErrCannotOpen)
	}
	return kit.Success(string(opened))
}
