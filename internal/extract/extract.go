// Package extract finds the parts of a message an automated reader came for:
// verification codes, and the links that act on an account.
//
// Most mail that reaches a public inbox was requested by a signup form, and
// whoever reads it — a person or an agent — wants exactly one thing out of it:
// a short code to type back, or a link to follow. Finding that thing on the
// server means every client gets the same answer, and an agent can go from
// "wait for mail" to "use the code" without parsing HTML at all.
//
// It is deliberately conservative. A wrong code is worse than no code: someone
// pastes it, is rejected, and trusts the feature less than if it had stayed
// quiet. So a candidate only counts when the words around it say it is a code,
// and anything that looks like part of a longer number, an amount, a date or an
// address is thrown away.
package extract

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

// Code is a verification code found in a message.
type Code struct {
	// Value is the code with any grouping removed: "123 456" becomes
	// "123456", which is what every form that asks for it expects.
	Value string
	// Context is the line it was found on, so a reader can check it.
	Context string
}

// LinkKind says what a link is for.
type LinkKind string

const (
	// LinkVerify acts on an account: confirm, verify, activate, sign in,
	// reset a password, accept an invitation.
	LinkVerify LinkKind = "verify"
	// LinkUnsubscribe stops mail. An agent following links must never
	// mistake one of these for the link it wanted.
	LinkUnsubscribe LinkKind = "unsubscribe"
	// LinkOther is everything else.
	LinkOther LinkKind = "other"
)

// Link is an http or https link found in a message.
type Link struct {
	URL  string
	Text string
	Kind LinkKind
}

// Result is everything found in one message.
type Result struct {
	Codes []Code
	Links []Link
}

const (
	maxCodes   = 3
	maxLinks   = 10
	maxContext = 160
	maxText    = 200
	// maxScan bounds the work done on one enormous body. The code or link a
	// signup email exists to deliver is near the top of it.
	maxScan = 64 << 10
)

var (
	// keywords are words that appear near a real code. A candidate on a line
	// without one — or without one on the line before — is ignored.
	keywords = regexp.MustCompile(`(?i)\b(code|codes|c[oó]digo|verify|verification|verifica\w*|otp|one[- ]time|pin|passcode|password|confirm\w*|authenticat\w*|security|token|log[- ]?in|sign[- ]?in|2fa|mfa|two[- ]factor)\b`)

	// strongLead is "code:" or "code is" directly before the candidate: the
	// shape almost every service uses to state the thing outright.
	strongLead = regexp.MustCompile(`(?i)(code|c[oó]digo|pin|otp|passcode)\s*(is|:|-)?\s*$`)

	// candidate matches 4–8 digits, a pair of digit groups split by a single
	// space or dash ("123 456", "123-456"), or 4–8 upper-case alphanumerics.
	candidate = regexp.MustCompile(`\b(\d{3,4}[ -]\d{3,4}|\d{4,8}|[A-Z0-9]{4,8})\b`)

	year     = regexp.MustCompile(`^(19|20)\d{2}$`)
	digitsRe = regexp.MustCompile(`^\d+$`)
	hasDigit = regexp.MustCompile(`\d`)

	bareURL = regexp.MustCompile(`https?://[^\s<>"'\x60\]\)]+`)

	unsubscribeRe = regexp.MustCompile(`(?i)unsubscribe|opt[-_ ]?out|email[-_ ]?preferences|notification[-_ ]?settings|manage[-_ ](your[-_ ])?(email|subscription|notification|preferences)`)
	verifyRe      = regexp.MustCompile(`(?i)verif|confirm|activat|validat|magic|sign[-_ ]?in|log[-_ ]?in|reset|password|authenticat|token=|code=|otp|invit|accept|join`)
)

// Message extracts codes and links from one message.
//
// text is the plain-text body and htmlBody the sanitized HTML one; either may
// be empty. When a message has only HTML, its text is derived from the HTML so
// the code search still has something to read.
func Message(subject, text, htmlBody string) Result {
	text = clip(text)
	htmlBody = clip(htmlBody)

	var anchors []Link
	if htmlBody != "" {
		var derived string
		derived, anchors = readHTML(htmlBody)
		if strings.TrimSpace(text) == "" {
			text = derived
		}
	}

	return Result{
		Codes: Codes(subject, text),
		Links: links(anchors, text),
	}
}

// Codes finds the most likely verification codes, best first.
func Codes(subject, text string) []Code {
	lines := append([]string{subject}, strings.Split(text, "\n")...)

	type scored struct {
		code  Code
		score int
		order int
	}
	var found []scored
	seen := map[string]bool{}

	for i, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		sameLine := keywords.MatchString(line)
		prevLine := i > 0 && keywords.MatchString(previousNonEmpty(lines, i))
		if !sameLine && !prevLine {
			continue
		}

		for _, loc := range candidate.FindAllStringSubmatchIndex(line, -1) {
			start, end := loc[2], loc[3]
			value, ok := acceptCandidate(line, start, end)
			if !ok || seen[value] {
				continue
			}
			seen[value] = true

			score := 0
			if sameLine {
				score += 2
			} else {
				score++
			}
			if strongLead.MatchString(line[:start]) {
				score += 3
			}
			if digitsRe.MatchString(value) {
				score++
			}
			if len(value) == 6 {
				score++
			}
			found = append(found, scored{
				code:  Code{Value: value, Context: truncate(line, maxContext)},
				score: score,
				order: len(found),
			})
		}
	}

	sort.SliceStable(found, func(a, b int) bool {
		if found[a].score != found[b].score {
			return found[a].score > found[b].score
		}
		return found[a].order < found[b].order
	})

	out := make([]Code, 0, min(len(found), maxCodes))
	for _, f := range found {
		if len(out) == maxCodes {
			break
		}
		out = append(out, f.code)
	}
	return out
}

// acceptCandidate decides whether a match is a code, and normalizes it.
func acceptCandidate(line string, start, end int) (string, bool) {
	raw := line[start:end]
	value := strings.NewReplacer(" ", "", "-", "").Replace(raw)

	if len(value) < 4 || len(value) > 8 {
		return "", false
	}
	if year.MatchString(value) {
		return "", false
	}
	// An upper-case run needs a digit in it. Without that rule every
	// shouted word in a subject line — FREE, SALE, HTML — is a candidate.
	if !hasDigit.MatchString(value) {
		return "", false
	}
	if partOfSomethingLonger(line, start, end) {
		return "", false
	}
	return value, true
}

// partOfSomethingLonger reports whether the match is a fragment of a phone
// number, an amount, a date, a URL, an email address, or a file name.
func partOfSomethingLonger(line string, start, end int) bool {
	before := func(n int) byte {
		if start-n < 0 {
			return 0
		}
		return line[start-n]
	}
	after := func(n int) byte {
		if end+n-1 >= len(line) {
			return 0
		}
		return line[end+n-1]
	}

	// Inside a URL, an address, a path, or an amount.
	switch before(1) {
	case '/', '=', '?', '&', '#', '@', '$', '+', '_', ':', '.', ',':
		// ':' and '.' before a candidate mean a time or a decimal: "10:30",
		// "3.1415". A code introduced by "code:" has a space after the colon.
		return true
	}
	// An amount in a currency whose symbol is more than one byte, with or
	// without a space: "€1234", "£ 1234".
	prefix := strings.TrimRight(line[:start], " ")
	for _, symbol := range []string{"€", "£", "¥", "₹", "₩", "USD", "EUR", "GBP"} {
		if strings.HasSuffix(prefix, symbol) {
			return true
		}
	}
	// Continuing a digit group: "415-555-1234", "1 234 567".
	if (before(1) == '-' || before(1) == ' ') && isDigit(before(2)) {
		return true
	}
	if (after(1) == '-' || after(1) == ' ' || after(1) == '.' || after(1) == ',' || after(1) == ':' || after(1) == '/') && isDigit(after(2)) {
		return true
	}
	// An address, a domain, or a file name: "abc123@x", "1234.com", "5678.pdf".
	if after(1) == '@' || (after(1) == '.' && isLetter(after(2))) {
		return true
	}
	// A percentage or a unit glued on: "1234%", "2048px", "1500ms".
	if after(1) == '%' || isLetter(after(1)) {
		return true
	}
	return false
}

// links merges anchors from the HTML with bare URLs from the text, classifies
// them, and orders them so the account-acting ones come first.
func links(anchors []Link, text string) []Link {
	var all []Link
	all = append(all, anchors...)
	for _, raw := range bareURL.FindAllString(text, -1) {
		all = append(all, Link{URL: strings.TrimRight(raw, ".,;:!?")})
	}

	seen := map[string]bool{}
	out := make([]Link, 0, len(all))
	for _, l := range all {
		u, ok := cleanURL(l.URL)
		if !ok || seen[u] {
			continue
		}
		seen[u] = true
		l.URL = u
		l.Text = truncate(strings.Join(strings.Fields(l.Text), " "), maxText)
		l.Kind = classify(l)
		out = append(out, l)
	}

	rank := map[LinkKind]int{LinkVerify: 0, LinkOther: 1, LinkUnsubscribe: 2}
	sort.SliceStable(out, func(a, b int) bool { return rank[out[a].Kind] < rank[out[b].Kind] })
	if len(out) > maxLinks {
		out = out[:maxLinks]
	}
	return out
}

// classify decides what a link is for from its text and its URL. The text is
// the stronger signal: click-tracking services rewrite every URL into the same
// opaque shape, but "Verify email" survives.
func classify(l Link) LinkKind {
	if unsubscribeRe.MatchString(l.Text) || unsubscribeRe.MatchString(l.URL) {
		return LinkUnsubscribe
	}
	if verifyRe.MatchString(l.Text) {
		return LinkVerify
	}
	if u, err := url.Parse(l.URL); err == nil && verifyRe.MatchString(u.Path+"?"+u.RawQuery) {
		return LinkVerify
	}
	return LinkOther
}

// cleanURL keeps only absolute http and https links.
func cleanURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	return u.String(), true
}

// readHTML walks sanitized HTML once, producing its visible text (with line
// breaks where blocks end) and its anchors.
func readHTML(body string) (string, []Link) {
	var (
		text    strings.Builder
		anchors []Link
		current *Link
		skip    int
	)
	z := html.NewTokenizer(strings.NewReader(body))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return text.String(), anchors

		case html.TextToken:
			if skip > 0 {
				continue
			}
			chunk := string(z.Text())
			text.WriteString(chunk)
			if current != nil {
				current.Text += chunk
			}

		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			switch tag {
			case "script", "style", "head", "title":
				skip++
			case "a":
				link := Link{}
				for hasAttr {
					var key, val []byte
					key, val, hasAttr = z.TagAttr()
					switch string(key) {
					case "href":
						link.URL = string(val)
					case "aria-label", "title":
						if link.Text == "" {
							link.Text = string(val)
						}
					}
				}
				if link.URL != "" {
					anchors = append(anchors, link)
					current = &anchors[len(anchors)-1]
				}
			case "br", "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6", "table", "blockquote", "hr":
				text.WriteByte('\n')
			case "td", "th":
				text.WriteByte(' ')
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style", "head", "title":
				if skip > 0 {
					skip--
				}
			case "a":
				current = nil
			case "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6", "table", "blockquote":
				text.WriteByte('\n')
			}
		}
	}
}

func previousNonEmpty(lines []string, i int) string {
	for j := i - 1; j >= 0; j-- {
		if s := strings.TrimSpace(lines[j]); s != "" {
			return s
		}
	}
	return ""
}

func clip(s string) string {
	if len(s) > maxScan {
		return s[:maxScan]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Cut on a rune boundary so the result is still valid UTF-8.
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
func isDigit(b byte) bool     { return b >= '0' && b <= '9' }
func isLetter(b byte) bool    { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
