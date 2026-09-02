package parse

import (
	"bytes"
	"errors"
	"io"
	"net/netip"
	"strings"

	"github.com/emersion/go-msgauth/dkim"

	"github.com/ethchor/phenk/internal/core"
)

// SPFFunc evaluates SPF for a delivery.
//
// It is a hook rather than an implementation because SPF needs a resolver, a
// macro expander and the ten-lookup limit, which is a dependency decision
// rather than a few lines of code. Until one is chosen, SPF is reported as
// none, which is what an unevaluated check honestly is — not a pass.
type SPFFunc func(ip netip.Addr, helo, envelopeFrom string) core.AuthResult

// verifyDKIM checks every signature on a message and returns the strongest
// outcome. A message with no signature is none, not fail: most legitimate mail
// still arrives unsigned.
func verifyDKIM(raw []byte) (core.AuthResult, []string) {
	verifications, err := dkim.Verify(bytes.NewReader(raw))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return core.AuthNone, nil
		}
		return core.AuthPermError, []string{"dkim: " + err.Error()}
	}
	if len(verifications) == 0 {
		return core.AuthNone, nil
	}

	var (
		result  = core.AuthFail
		domains []string
	)
	for _, v := range verifications {
		if v.Err == nil {
			// One valid signature is enough: a message can carry several, and
			// intermediaries routinely add their own.
			result = core.AuthPass
			domains = append(domains, v.Domain)
			continue
		}
		// A temporary failure — usually a DNS lookup that did not answer — is
		// not evidence the message is forged, so it must not be reported as a
		// failure that a reader would act on.
		if dkim.IsTempFail(v.Err) && result != core.AuthPass {
			result = core.AuthTempError
		}
	}
	return result, domains
}

// deriveDMARC applies DMARC's alignment rule to the SPF and DKIM outcomes.
//
// DMARC passes when at least one of the two passes and its identifier aligns
// with the From domain. Without a policy lookup this cannot distinguish a
// domain that publishes no DMARC record from one that publishes p=none, so a
// message with nothing to align reports none rather than fail: reporting fail
// would mark most legitimate mail as suspicious.
func deriveDMARC(fromAddr string, spf core.AuthResult, envelopeFrom string, dkimResult core.AuthResult, dkimDomains []string) core.AuthResult {
	fromDomain := domainOf(fromAddr)
	if fromDomain == "" {
		return core.AuthNone
	}

	if dkimResult == core.AuthPass {
		for _, d := range dkimDomains {
			if aligned(fromDomain, strings.ToLower(d)) {
				return core.AuthPass
			}
		}
	}
	if spf == core.AuthPass && aligned(fromDomain, domainOf(envelopeFrom)) {
		return core.AuthPass
	}

	if spf == core.AuthNone && dkimResult == core.AuthNone {
		return core.AuthNone
	}
	return core.AuthFail
}

// aligned implements relaxed alignment: the identifier's organisational domain
// must match the From domain's. Without the public suffix list this uses the
// last two labels, which is right for the common cases and wrong for suffixes
// like co.uk — a limitation worth naming rather than hiding.
func aligned(fromDomain, identifier string) bool {
	if identifier == "" {
		return false
	}
	return fromDomain == identifier ||
		strings.HasSuffix(fromDomain, "."+identifier) ||
		strings.HasSuffix(identifier, "."+fromDomain)
}

func domainOf(address string) string {
	i := strings.LastIndexByte(address, '@')
	if i < 0 || i == len(address)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(address[i+1:]))
}
