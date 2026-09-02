package core

import (
	"crypto/rand"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
)

// UUID is an RFC 4122 universally unique identifier.
//
// Generated values are version 7: a 48-bit big-endian millisecond timestamp
// followed by random bits. Time ordering keeps primary key inserts local in
// the btree, which matters most for deliveries, the highest-volume table.
type UUID [16]byte

// NilUUID is the all-zero UUID, used as the zero value.
var NilUUID UUID

// NewUUID returns a new version 7 UUID. It panics if the system random source
// fails, which is the same posture the standard library takes for key
// generation: there is no safe way to continue without entropy.
func NewUUID() UUID {
	var u UUID
	if _, err := rand.Read(u[6:]); err != nil {
		panic("core: random source unavailable: " + err.Error())
	}
	ms := nowMillis()
	u[0] = byte(ms >> 40)
	u[1] = byte(ms >> 32)
	u[2] = byte(ms >> 24)
	u[3] = byte(ms >> 16)
	u[4] = byte(ms >> 8)
	u[5] = byte(ms)
	u[6] = (u[6] & 0x0f) | 0x70 // version 7
	u[8] = (u[8] & 0x3f) | 0x80 // RFC 4122 variant
	return u
}

// IsZero reports whether u is the nil UUID.
func (u UUID) IsZero() bool { return u == NilUUID }

// String returns the canonical 8-4-4-4-12 hyphenated form.
func (u UUID) String() string {
	var b [36]byte
	hex.Encode(b[0:8], u[0:4])
	b[8] = '-'
	hex.Encode(b[9:13], u[4:6])
	b[13] = '-'
	hex.Encode(b[14:18], u[6:8])
	b[18] = '-'
	hex.Encode(b[19:23], u[8:10])
	b[23] = '-'
	hex.Encode(b[24:36], u[10:16])
	return string(b[:])
}

// ParseUUID accepts the canonical hyphenated form and the unhyphenated form.
func ParseUUID(s string) (UUID, error) {
	var u UUID
	switch len(s) {
	case 36:
		if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
			return u, fmt.Errorf("core: malformed uuid %q", s)
		}
		src := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36]
		if _, err := hex.Decode(u[:], []byte(src)); err != nil {
			return UUID{}, fmt.Errorf("core: malformed uuid %q", s)
		}
	case 32:
		if _, err := hex.Decode(u[:], []byte(s)); err != nil {
			return UUID{}, fmt.Errorf("core: malformed uuid %q", s)
		}
	default:
		return u, fmt.Errorf("core: malformed uuid %q", s)
	}
	return u, nil
}

// Value implements driver.Valuer so a UUID can be used as a query argument.
func (u UUID) Value() (driver.Value, error) { return u.String(), nil }

// Scan implements sql.Scanner so a uuid column can be read into a UUID.
func (u *UUID) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*u = NilUUID
		return nil
	case string:
		parsed, err := ParseUUID(v)
		if err != nil {
			return err
		}
		*u = parsed
		return nil
	case []byte:
		if len(v) == 16 {
			copy(u[:], v)
			return nil
		}
		parsed, err := ParseUUID(string(v))
		if err != nil {
			return err
		}
		*u = parsed
		return nil
	default:
		return errors.New("core: cannot scan uuid from " + fmt.Sprintf("%T", src))
	}
}

// MarshalText implements encoding.TextMarshaler.
func (u UUID) MarshalText() ([]byte, error) { return []byte(u.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (u *UUID) UnmarshalText(b []byte) error {
	parsed, err := ParseUUID(string(b))
	if err != nil {
		return err
	}
	*u = parsed
	return nil
}
