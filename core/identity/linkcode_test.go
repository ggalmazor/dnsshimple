package identity

import "testing"

func TestTypedCodesNormalizeToTheStoredForm(test *testing.T) {
	// Given a freshly issued link
	link := NewPendingLink("SHA256:some-key")

	// When the user types it back in any reasonable shape
	for _, typed := range []string{
		link.Code,
		DisplayLinkCode(link.Code),
		"  " + DisplayLinkCode(link.Code) + " ",
	} {
		// Then it normalizes to the stored form
		if NormalizeLinkCode(typed) != link.Code {
			test.Fatalf("%q did not normalize to %q", typed, link.Code)
		}
	}
}

func TestIssuedCodesUseOnlyTheUnambiguousAlphabet(test *testing.T) {
	// Given many issued codes (When: issuing is the action under test)
	for range 200 {
		code := NewPendingLink("SHA256:some-key").Code

		// Then each is 8 characters from the lookalike-free alphabet
		if len(code) != linkCodeLength {
			test.Fatalf("code %q is not %d characters", code, linkCodeLength)
		}
		if NormalizeLinkCode(code) != code {
			test.Fatalf("code %q contains characters outside the alphabet", code)
		}
	}
}
