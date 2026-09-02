package core

import (
	"errors"
	"regexp"
	"strings"
)

// RandomAlphabet is Crockford base32 in lower case: the digits and letters
// with I, L, O and U removed so a generated address cannot be misread or
// mistyped. It lives here rather than in the allocator because the named-address
// validator has to recognise a generated address in order to refuse it, and two
// copies of this alphabet would eventually drift apart.
const RandomAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// RandomLocalPartLen is the length of a generated local part. Ten Crockford
// base32 characters is 50 bits of entropy.
const RandomLocalPartLen = 10

// Errors returned by address validation. They are deliberately distinguishable
// so the SMTP layer can map them to different replies, while every caller that
// reports to a remote party collapses them into one message.
var (
	ErrLocalPartSyntax    = errors.New("core: local part is not a valid name")
	ErrLocalPartReserved  = errors.New("core: local part is reserved")
	ErrLocalPartBlocked   = errors.New("core: local part is blocked")
	ErrAddressSyntax      = errors.New("core: malformed address")
	ErrKindPoolMismatch   = errors.New("core: identity kind does not match domain pool")
	ErrNotEligibleForAuth = errors.New("core: named identities cannot hold scoped access")
)

// namedLocalPart is the §6.5 grammar, applied identically on the SMTP and HTTP
// paths.
var namedLocalPart = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)

// NormalizeLocalPart lower-cases a local part and strips any +tag. Both the
// SMTP and HTTP paths normalize before doing anything else, so invoices+foo@
// and INVOICES@ reach the same inbox.
func NormalizeLocalPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	return s
}

// SplitAddress splits an addr-spec into its normalized local part and domain.
// It does not validate the local part: callers do that with the validator that
// matches the domain's pool.
func SplitAddress(addr string) (local, domain string, err error) {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(addr, "<")
	addr = strings.TrimSuffix(addr, ">")
	i := strings.LastIndexByte(addr, '@')
	if i <= 0 || i == len(addr)-1 {
		return "", "", ErrAddressSyntax
	}
	local = NormalizeLocalPart(addr[:i])
	domain = strings.ToLower(strings.TrimSpace(addr[i+1:]))
	if local == "" || domain == "" || strings.ContainsAny(domain, " \t@") {
		return "", "", ErrAddressSyntax
	}
	return local, domain, nil
}

// LooksGenerated reports whether s has the exact shape of a generated random
// address. Named inboxes live on different domains than generated ones, so a
// name could not shadow a generated address even if this returned false — but
// refusing the shape outright means a change to the domain pools can never
// turn that into a real collision.
func LooksGenerated(s string) bool {
	if len(s) != RandomLocalPartLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune(RandomAlphabet, rune(s[i])) {
			return false
		}
	}
	return true
}

// ValidateNamedLocalPart applies the §6.5 grammar to an already-normalized
// local part. It is the syntactic half of validation; the blocklist lookup is
// the storage layer's half, because the list is data that changes without a
// deploy. Both the SMTP RCPT TO path and POST /v1/named call this same
// function, which is the point of it.
func ValidateNamedLocalPart(s string) error {
	if !namedLocalPart.MatchString(s) {
		return ErrLocalPartSyntax
	}
	if LooksGenerated(s) {
		return ErrLocalPartReserved
	}
	return nil
}
