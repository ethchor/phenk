package pg

import "time"

// nullTime lets a caller leave a timestamp unset and take the database default
// instead of writing a zero time.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// nullString maps Go's empty string to SQL NULL, for the columns where "not
// set" and "set to empty" must not be confused — owner_session above all,
// which a CHECK constraint requires to be NULL on named identities.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// textOrEmpty reads a nullable text column into a plain string.
func textOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
