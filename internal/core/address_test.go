package core

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeLocalPart(t *testing.T) {
	cases := map[string]string{
		"Invoices":       "invoices",
		"  invoices  ":   "invoices",
		"invoices+ebay":  "invoices",
		"INVOICES+A+B":   "invoices",
		"invoices+":      "invoices",
		"+leading":       "",
		"MixedCase.Name": "mixedcase.name",
	}
	for in, want := range cases {
		if got := NormalizeLocalPart(in); got != want {
			t.Errorf("NormalizeLocalPart(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitAddress(t *testing.T) {
	local, domain, err := SplitAddress("<Invoices+ebay@Example.COM>")
	if err != nil {
		t.Fatalf("SplitAddress: %v", err)
	}
	if local != "invoices" || domain != "example.com" {
		t.Fatalf("got %q@%q, want invoices@example.com", local, domain)
	}

	for _, bad := range []string{"", "no-at-sign", "@example.com", "user@", "a@b c", "user@@"} {
		if _, _, err := SplitAddress(bad); err == nil {
			t.Errorf("SplitAddress(%q) = nil error, want failure", bad)
		}
	}
}

func TestValidateNamedLocalPart(t *testing.T) {
	valid := []string{"abc", "invoices", "my.name", "my-name", "my_name", "a1b2", strings.Repeat("a", 64)}
	for _, s := range valid {
		if err := ValidateNamedLocalPart(s); err != nil {
			t.Errorf("ValidateNamedLocalPart(%q) = %v, want nil", s, err)
		}
	}

	invalid := []string{
		"",                      // empty
		"ab",                    // too short
		strings.Repeat("a", 65), // too long
		".leading",              // must start alphanumeric
		"-leading",              // must start alphanumeric
		"has space",             // no spaces
		"has@at",                // no at sign
		"Uppercase",             // must be normalized first
		"unicodé",               // ascii only
	}
	for _, s := range invalid {
		if err := ValidateNamedLocalPart(s); !errors.Is(err, ErrLocalPartSyntax) {
			t.Errorf("ValidateNamedLocalPart(%q) = %v, want ErrLocalPartSyntax", s, err)
		}
	}
}

func TestValidateNamedLocalPartRejectsGeneratedShape(t *testing.T) {
	// A name that looks exactly like a generated address is refused even though
	// the two live on different domain pools. Belt and braces, per §6.5.
	if err := ValidateNamedLocalPart("k7f2m9x3qz"); !errors.Is(err, ErrLocalPartReserved) {
		t.Fatalf("got %v, want ErrLocalPartReserved", err)
	}
	// Nine characters is not the generated shape, so it is an ordinary name.
	if err := ValidateNamedLocalPart("k7f2m9x3q"); err != nil {
		t.Fatalf("got %v, want nil", err)
	}
	// Ten characters containing an excluded Crockford letter is not the shape
	// either: a generated address can never contain an i, l, o or u.
	if err := ValidateNamedLocalPart("k7f2m9x3qi"); err != nil {
		t.Fatalf("got %v, want nil", err)
	}
}

func TestLooksGenerated(t *testing.T) {
	if !LooksGenerated("0123456789") {
		t.Error("LooksGenerated(0123456789) = false, want true")
	}
	for _, s := range []string{"", "short", "0123456789a", "0123456i89"} {
		if LooksGenerated(s) {
			t.Errorf("LooksGenerated(%q) = true, want false", s)
		}
	}
}

func TestRandomAlphabetExcludesAmbiguousCharacters(t *testing.T) {
	if len(RandomAlphabet) != 32 {
		t.Fatalf("alphabet is %d characters, want 32", len(RandomAlphabet))
	}
	for _, c := range "ilou" {
		if strings.ContainsRune(RandomAlphabet, c) {
			t.Errorf("alphabet contains ambiguous character %q", c)
		}
	}
}
