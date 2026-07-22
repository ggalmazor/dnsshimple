package vault

import (
	"errors"
	"testing"
)

func TestSealedTokensOpenBackToTheOriginal(test *testing.T) {
	// Given a vault
	vault, err := FromBase64Key(NewRandomBase64Key())
	if err != nil {
		test.Fatalf("building the vault: %v", err)
	}

	// When a token is sealed and opened
	sealed := vault.Seal("dnsimple-access-token")
	if sealed.Failed() {
		test.Fatalf("sealing failed: %v", sealed.Error())
	}
	opened := vault.Open(sealed.Value())

	// Then the round trip is lossless
	if opened.Failed() || opened.Value() != "dnsimple-access-token" {
		test.Fatalf("round trip broke: %v / %q", opened.Error(), opened.Value())
	}
}

func TestTamperedOrForeignCiphertextsDoNotOpen(test *testing.T) {
	// Given two vaults with different keys and a sealed token
	firstVault, _ := FromBase64Key(NewRandomBase64Key())
	secondVault, _ := FromBase64Key(NewRandomBase64Key())
	sealed := firstVault.Seal("dnsimple-access-token").Value()

	// When the ciphertext is tampered with
	tampered := append([]byte{}, sealed...)
	tampered[len(tampered)-1] ^= 0xFF

	// Then neither the tampered box nor the wrong key opens, and both fail
	// with the same undifferentiated error
	for name, attempt := range map[string][]byte{
		"tampered box": tampered,
		"too short":    sealed[:10],
	} {
		if outcome := firstVault.Open(attempt); !outcome.Failed() || !errors.Is(outcome.Error(), ErrCannotOpen) {
			test.Fatalf("%s must fail with ErrCannotOpen, got %v", name, outcome.Error())
		}
	}
	if outcome := secondVault.Open(sealed); !outcome.Failed() || !errors.Is(outcome.Error(), ErrCannotOpen) {
		test.Fatalf("a foreign key must fail with ErrCannotOpen, got %v", outcome.Error())
	}
}
