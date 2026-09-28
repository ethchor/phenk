package extract

import (
	"reflect"
	"testing"
)

func codeValues(codes []Code) []string {
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		out = append(out, c.Value)
	}
	return out
}

func TestFindsCodesWhereServicesPutThem(t *testing.T) {
	cases := []struct {
		name          string
		subject, body string
		want          string
	}{
		{"in the subject", "482913 is your Acme verification code", "Welcome!", "482913"},
		{"stated outright", "Welcome", "Your verification code is 482913.", "482913"},
		{"after a colon", "Sign in", "Your code: 736104", "736104"},
		{"on the line after the keyword", "Sign in", "Use this code to sign in:\n\n    736104\n\nIt expires soon.", "736104"},
		{"grouped with a space", "Sign in", "Your code is 736 104", "736104"},
		{"grouped with a dash", "Sign in", "Your code: 123-456", "123456"},
		{"alphanumeric", "Your login", "Your one-time passcode is A7K2QX", "A7K2QX"},
		{"Spanish", "Bienvenido", "Tu código de verificación es 553201", "553201"},
		{"with an expiry beside it", "Verify", "Your code is 482913. It expires in 10 minutes.", "482913"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Codes(tc.subject, tc.body)
			if len(got) == 0 || got[0].Value != tc.want {
				t.Fatalf("Codes() = %v, want %q first", codeValues(got), tc.want)
			}
			if got[0].Context == "" {
				t.Error("a code must carry the line it came from, so a reader can check it")
			}
		})
	}
}

// Every case here is something a naive "find a 6-digit number" pass returns.
// Each one would be pasted into a form, rejected, and cost the feature trust.
func TestIgnoresThingsThatOnlyLookLikeCodes(t *testing.T) {
	cases := []struct {
		name          string
		subject, body string
	}{
		{"an amount", "Confirm your order", "Your order total is $1234.00. Confirm your order below."},
		{"an amount in euros", "Confirm your order", "Total: € 4821 — please confirm."},
		{"a phone number", "Account security", "Call us at 415-555-1234 about your account security."},
		{"a year", "Security notice", "© 2026 Acme Inc. Security matters to us."},
		{"a time", "Confirm attendance", "The meeting is at 10:30. Confirm attendance."},
		{"a date", "Your code", "Your code expires 2026-09-28."},
		{"no keyword nearby", "Invoice", "Invoice 48291300 is attached."},
		{"inside a URL", "Confirm", "Confirm here: https://acme.test/confirm/ABCD1234"},
		{"inside an address", "Code sent", "We sent a code to USER1234@EXAMPLE.COM"},
		{"a shouted word", "VERIFY YOUR ACCOUNT", "VERIFY NOW"},
		{"a size", "Security", "Use a 2048px image for your security badge."},
		{"a percentage", "Code", "Use code SAVE20 for 1500% off"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// SAVE20 is a real promo code, which is exactly the problem: the
			// assertion is only that the percentage is not mistaken for one.
			for _, c := range Codes(tc.subject, tc.body) {
				if c.Value == "1500" {
					t.Fatalf("returned a percentage as a code: %+v", c)
				}
				if tc.name != "a percentage" {
					t.Fatalf("returned %q from %q", c.Value, tc.body)
				}
			}
		})
	}
}

func TestPrefersTheCodeTheMessageNamesOutright(t *testing.T) {
	body := "Reference 4829 1734 for your records.\nYour verification code is 993211."
	got := Codes("Verify your account", body)
	if len(got) == 0 || got[0].Value != "993211" {
		t.Fatalf("Codes() = %v, want the stated code first", codeValues(got))
	}
}

func TestReadsCodesOutOfHTMLOnlyMail(t *testing.T) {
	html := `<html><head><style>.x{}</style></head><body>` +
		`<table><tr><td><p>Your code is</p><p><strong>481920</strong></p></td></tr></table>` +
		`</body></html>`
	got := Message("Verify", "", html)
	if len(got.Codes) == 0 || got.Codes[0].Value != "481920" {
		t.Fatalf("Codes = %v, want 481920 from the HTML", codeValues(got.Codes))
	}
}

func TestClassifiesAndOrdersLinks(t *testing.T) {
	html := `<p><a href="https://acme.test/blog">Read our blog</a></p>` +
		`<p><a href="https://acme.test/unsubscribe?u=1">Unsubscribe</a></p>` +
		`<p><a href="https://acme.test/verify?token=abc">Verify your email</a></p>` +
		// Click tracking rewrites every URL into the same opaque shape. The
		// anchor text is what still says what the link does.
		`<p><a href="https://click.mailer.test/ls/click?upn=xyz">Confirm your account</a></p>` +
		`<p><a href="javascript:alert(1)">Click</a> <a href="mailto:help@acme.test">Help</a></p>`
	got := Message("Welcome", "", html).Links

	want := []Link{
		{URL: "https://acme.test/verify?token=abc", Text: "Verify your email", Kind: LinkVerify},
		{URL: "https://click.mailer.test/ls/click?upn=xyz", Text: "Confirm your account", Kind: LinkVerify},
		{URL: "https://acme.test/blog", Text: "Read our blog", Kind: LinkOther},
		{URL: "https://acme.test/unsubscribe?u=1", Text: "Unsubscribe", Kind: LinkUnsubscribe},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("links =\n%+v\nwant\n%+v", got, want)
	}
}

func TestFindsBareLinksInTextAndDeduplicates(t *testing.T) {
	text := "Sign in at https://acme.test/login.\nOr again: https://acme.test/login"
	html := `<a href="https://acme.test/login">Sign in</a>`
	got := Message("Sign in", text, html).Links
	if len(got) != 1 {
		t.Fatalf("links = %+v, want one deduplicated link", got)
	}
	if got[0].URL != "https://acme.test/login" || got[0].Kind != LinkVerify {
		t.Fatalf("link = %+v, want the sign-in link classified as verify", got[0])
	}
}

func TestEmptyMessagesProduceEmptyNotNil(t *testing.T) {
	// The API renders these as JSON arrays. A nil slice would render as null,
	// and every client would need a special case for it.
	got := Message("", "", "")
	if got.Codes == nil || got.Links == nil {
		t.Fatalf("got nil slices: %+v", got)
	}
}

func TestTruncateKeepsUTF8Valid(t *testing.T) {
	s := truncate("código código código", 7)
	for _, r := range s {
		if r == '�' {
			t.Fatalf("truncate split a rune: %q", s)
		}
	}
}
