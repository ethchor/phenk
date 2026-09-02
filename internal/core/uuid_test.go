package core

import (
	"sort"
	"testing"
)

func TestUUIDRoundTrip(t *testing.T) {
	u := NewUUID()
	if u.IsZero() {
		t.Fatal("NewUUID returned the nil uuid")
	}
	if got := u.String(); len(got) != 36 {
		t.Fatalf("String() = %q, want 36 characters", got)
	}
	back, err := ParseUUID(u.String())
	if err != nil {
		t.Fatalf("ParseUUID: %v", err)
	}
	if back != u {
		t.Fatalf("round trip changed the value: %v != %v", back, u)
	}
	unhyphenated := u.String()[0:8] + u.String()[9:13] + u.String()[14:18] + u.String()[19:23] + u.String()[24:36]
	if back, err = ParseUUID(unhyphenated); err != nil || back != u {
		t.Fatalf("unhyphenated parse: %v %v", back, err)
	}
}

func TestUUIDVersionAndVariant(t *testing.T) {
	u := NewUUID()
	if v := u[6] >> 4; v != 7 {
		t.Errorf("version = %d, want 7", v)
	}
	if variant := u[8] >> 6; variant != 0b10 {
		t.Errorf("variant = %b, want 10", variant)
	}
}

func TestUUIDIsTimeOrdered(t *testing.T) {
	// Version 7 exists so primary key inserts stay local in the btree. Within a
	// single millisecond order is arbitrary, so compare only the timestamp
	// prefix across a batch that certainly spans more than one.
	const n = 200
	ids := make([]UUID, n)
	for i := range ids {
		ids[i] = NewUUID()
	}
	prefixes := make([]string, n)
	for i, id := range ids {
		prefixes[i] = string(id[:6])
	}
	if !sort.SliceIsSorted(prefixes, func(a, b int) bool { return prefixes[a] < prefixes[b] }) {
		t.Fatal("generated uuids are not time ordered")
	}
}

func TestUUIDUniqueness(t *testing.T) {
	seen := make(map[UUID]bool, 10000)
	for i := 0; i < 10000; i++ {
		u := NewUUID()
		if seen[u] {
			t.Fatalf("duplicate uuid after %d draws", i)
		}
		seen[u] = true
	}
}

func TestUUIDParseRejectsMalformed(t *testing.T) {
	for _, s := range []string{"", "nope", "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz", "0123456789abcdef0123456789abcdefff"} {
		if _, err := ParseUUID(s); err == nil {
			t.Errorf("ParseUUID(%q) = nil error, want failure", s)
		}
	}
}

func TestUUIDScanAndValue(t *testing.T) {
	u := NewUUID()
	v, err := u.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	var back UUID
	if err := back.Scan(v); err != nil {
		t.Fatalf("Scan(string): %v", err)
	}
	if back != u {
		t.Fatal("scan of Value did not round trip")
	}
	if err := back.Scan(u[:]); err != nil || back != u {
		t.Fatalf("Scan([]byte of 16): %v", err)
	}
	if err := back.Scan(nil); err != nil || !back.IsZero() {
		t.Fatalf("Scan(nil): %v", err)
	}
	if err := back.Scan(42); err == nil {
		t.Fatal("Scan(int) = nil error, want failure")
	}
}
