package alloc

import (
	"strings"
	"testing"

	"github.com/ethchor/phenk/internal/core"
)

func TestGenerateLocalPartShape(t *testing.T) {
	for i := 0; i < 500; i++ {
		s := GenerateLocalPart()
		if len(s) != core.RandomLocalPartLen {
			t.Fatalf("generated %q, want %d characters", s, core.RandomLocalPartLen)
		}
		for _, c := range s {
			if !strings.ContainsRune(core.RandomAlphabet, c) {
				t.Fatalf("generated %q, which contains %q, outside the Crockford alphabet", s, c)
			}
		}
		// A generated address must be recognisable as one, so a named inbox
		// can never be made to look like it.
		if !core.LooksGenerated(s) {
			t.Fatalf("generated %q is not recognised as a generated address", s)
		}
	}
}

func TestGenerateLocalPartIsUnpredictable(t *testing.T) {
	seen := make(map[string]bool, 20000)
	for i := 0; i < 20000; i++ {
		s := GenerateLocalPart()
		if seen[s] {
			t.Fatalf("generated a duplicate address after %d draws", i)
		}
		seen[s] = true
	}
}

func TestGenerateLocalPartUsesTheWholeAlphabet(t *testing.T) {
	// Reducing a random byte modulo 32 is uniform because the alphabet divides
	// 256 exactly. If that ever stops being true this test notices: a biased
	// generator leaves characters unused.
	counts := map[rune]int{}
	const draws = 5000
	for i := 0; i < draws; i++ {
		for _, c := range GenerateLocalPart() {
			counts[c]++
		}
	}
	if len(counts) != len(core.RandomAlphabet) {
		t.Fatalf("only %d of %d alphabet characters appeared", len(counts), len(core.RandomAlphabet))
	}
	total := draws * core.RandomLocalPartLen
	expected := total / len(core.RandomAlphabet)
	for c, n := range counts {
		if n < expected/2 || n > expected*2 {
			t.Errorf("character %q appeared %d times, expected around %d", c, n, expected)
		}
	}
}

func TestPickIndexStaysInRange(t *testing.T) {
	if got := pickIndex(1); got != 0 {
		t.Fatalf("pickIndex(1) = %d", got)
	}
	if got := pickIndex(0); got != 0 {
		t.Fatalf("pickIndex(0) = %d", got)
	}
	for i := 0; i < 1000; i++ {
		if got := pickIndex(7); got < 0 || got > 6 {
			t.Fatalf("pickIndex(7) = %d, out of range", got)
		}
	}
}
