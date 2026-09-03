#!/usr/bin/env bash
# Regression tests for scripts/cloudflare-dns.sh, run against a stand-in API.
#
# Two of these exist because the script shipped with the bug they describe. The
# apex TXT case is the one that mattered: the script would rewrite a stranger's
# domain-verification record into our SPF value, under --apply, while reporting
# it as an ordinary update.

set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
script="$here/cloudflare-dns.sh"
stub="$here/testdata/fake-cloudflare-api.py"

export CLOUDFLARE_API_BASE="http://127.0.0.1:8787/client/v4"
export CLOUDFLARE_API_TOKEN="test-token"

log=""
srv=""
failures=0

# start [--routing on|off] [stub args...]
start() {
	routing=off
	if [ "${1:-}" = "--routing" ]; then
		routing="$2"
		shift 2
	fi
	log="$(mktemp)"
	ROUTING="$routing" python3 "$stub" "$@" >"$log" 2>&1 &
	srv=$!
	# The stub prints "listening" once bound. Waiting for the line rather than
	# sleeping keeps the suite fast and removes the flake.
	for _ in $(seq 1 100); do
		grep -q listening "$log" 2>/dev/null && return 0
		sleep 0.1
	done
	echo "stub API did not start" >&2
	exit 1
}

stop() {
	[ -n "$srv" ] || return 0
	kill "$srv" 2>/dev/null || true
	wait "$srv" 2>/dev/null || true
	rm -f "$log"
	srv=""
}
trap stop EXIT

ok() { printf '  ok    %s\n' "$1"; }
fail() {
	printf '  FAIL  %s\n' "$1" >&2
	printf '        %s\n' "$2" >&2
	failures=$((failures + 1))
}

apex_txt() {
	curl -sS "$CLOUDFLARE_API_BASE/zones/zone123/dns_records?type=TXT&name=example.com" \
		-H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" | jq -r '.result[].content' | sort
}

run() { "$script" --domain example.com --ip 203.0.113.10 "$@" 2>&1; }

# The script must reach the first record at all. It once did not: the default
# for the optional JSON argument relied on backslash handling inside a
# default-value expansion, which bash 3.2 and bash 5 disagree about, and the
# failure landed before any record was touched — including in a dry run.
start
out="$(run || true)"
case "$out" in
*"create  A"*) ok "reaches the first record" ;;
*) fail "reaches the first record" "$(printf '%s' "$out" | head -3)" ;;
esac
stop

# The destructive one. An apex holding a verification record and a correct SPF
# record must come back reporting the SPF as already right, and must leave the
# verification record exactly as it was.
start --seed-apex-txt
out="$(run || true)"
case "$out" in
*"ok      TXT  example.com"*) ok "recognises an already-correct apex SPF record" ;;
*) fail "recognises an already-correct apex SPF record" "$(printf '%s' "$out" | grep TXT || true)" ;;
esac

run --apply >/dev/null 2>&1 || true
got="$(apex_txt)"
want="$(printf 'google-site-verification=abc123\nv=spf1 -all')"
if [ "$got" = "$want" ]; then
	ok "leaves a foreign apex TXT record untouched under --apply"
else
	fail "leaves a foreign apex TXT record untouched under --apply" "got: $(printf '%s' "$got" | tr '\n' '|')"
fi
stop

# The same apex without our SPF record: it must be created alongside, never by
# commandeering the record that is already there.
start --seed-apex-txt --no-spf
run --apply >/dev/null 2>&1 || true
got="$(apex_txt)"
if [ "$got" = "$want" ]; then
	ok "creates the apex SPF record without taking over another one"
else
	fail "creates the apex SPF record without taking over another one" "got: $(printf '%s' "$got" | tr '\n' '|')"
fi
stop

# The API quotes TXT content in some responses and not others. A quoted read of
# an identical record must not look like a change, or the script never converges.
start --seed-apex-txt --quoted-txt
out="$(run || true)"
case "$out" in
*"ok      TXT  example.com"*) ok "treats quoted and bare TXT content as equal" ;;
*) fail "treats quoted and bare TXT content as equal" "$(printf '%s' "$out" | grep 'TXT  example.com' || true)" ;;
esac
stop

# Applying twice must be a no-op. `proxied: false` read through jq's `//`
# operator once made every record look changed on every run.
start
run --apply >/dev/null 2>&1 || true
out="$(run --apply || true)"
case "$out" in
*"everything already matches"*) ok "is idempotent" ;;
*) fail "is idempotent" "$(printf '%s' "$out" | tail -3)" ;;
esac
stop

# The reason the script exists: a proxied mail host means Cloudflare answers
# port 25 and never passes it on.
start --seed-proxied-mx-host
out="$(run || true)"
case "$out" in
*"update  A    mx1.example.com"*) ok "un-proxies a proxied mail host" ;;
*) fail "un-proxies a proxied mail host" "$(printf '%s' "$out" | grep mx1 || true)" ;;
esac
stop

# Email Routing manages and locks the apex MX and SPF records.
start --routing on
if run >/dev/null 2>&1; then
	fail "refuses to run when Email Routing is on" "exited 0"
else
	ok "refuses to run when Email Routing is on"
fi
stop

# A public-pool domain hosts mail and nothing else.
start
out="$(run --public || true)"
case "$out" in
*app.example.com*) fail "omits the app record for a public-pool domain" "app record present" ;;
*) ok "omits the app record for a public-pool domain" ;;
esac
stop

echo
if [ "$failures" -eq 0 ]; then
	echo "cloudflare-dns: all checks passed"
else
	echo "cloudflare-dns: $failures check(s) failed" >&2
	exit 1
fi
