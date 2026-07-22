package identity

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

const (
	linkCodeAlphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789" // no 0/O, 1/I/L lookalikes
	linkCodeLength   = 8
	// LinkCodeLifetime bounds how long a code shown in a terminal stays
	// redeemable; ConsumePendingLink enforces it.
	LinkCodeLifetime = 10 * time.Minute
)

// NewPendingLink issues a fresh single-use code for an unlinked fingerprint.
func NewPendingLink(fingerprint string) PendingLink {
	return PendingLink{
		Code:        newLinkCode(),
		Fingerprint: fingerprint,
		StartedAt:   time.Now(),
	}
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
		// No entropy means no security property holds anywhere; stop the process.
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	var code strings.Builder
	for _, randomByte := range randomBytes {
		code.WriteByte(linkCodeAlphabet[int(randomByte)%len(linkCodeAlphabet)])
	}
	return code.String()
}
