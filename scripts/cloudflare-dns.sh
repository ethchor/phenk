#!/usr/bin/env bash
# Apply Phenk's DNS records to a Cloudflare zone.
#
# The records themselves are documented in docs/deployment.md, which explains
# why each one is shaped the way it is. This script is that table, executable
# and idempotent, so the mail records cannot be got subtly wrong by hand.
#
# The one mistake this exists to prevent is proxying a mail record. Cloudflare
# does not carry SMTP through the proxy, so an orange-clouded A record for the
# mail host means no mail arrives at all — and the failure is silent, because
# DNS resolves fine and the connection is simply answered by something that is
# not your mail server. Every record this script creates for mail is forced to
# DNS-only, and it will refuse to leave an existing one proxied.
#
# Usage:
#   export CLOUDFLARE_API_TOKEN=...        # never pass this as an argument
#   scripts/cloudflare-dns.sh --domain example.com --ip 203.0.113.10
#   scripts/cloudflare-dns.sh --domain example.com --ip 203.0.113.10 --apply
#
# Without --apply it prints what it would change and touches nothing.
#
# The token needs exactly one permission: Zone > DNS > Edit, scoped to the zone
# you are changing. Do not use a global API key.

set -euo pipefail

# Overridable so the script can be exercised against a stand-in API. There is
# no reason to set this against a real account.
api="${CLOUDFLARE_API_BASE:-https://api.cloudflare.com/client/v4}"

domain=""
ip=""
mail_host="mx1"
app_host="app"
dmarc_email=""
mx_priority=10
apply=0
public_pool=0

usage() {
	cat >&2 <<'EOF'
usage: cloudflare-dns.sh --domain <zone> --ip <address> [options]

  --domain <zone>       the zone to change, e.g. example.com
  --ip <address>        the mail host's IPv4 address
  --mail-host <label>   hostname label for the MX target (default: mx1)
  --app-host <label>    hostname label for the inbox app (default: app)
  --dmarc-email <addr>  where DMARC reports go (default: dmarc@<zone>)
  --priority <n>        MX priority (default: 10)
  --public              a public-pool domain: mail records only, no app record
  --apply               actually make the changes (default: dry run)

CLOUDFLARE_API_TOKEN must be set in the environment. It needs Zone > DNS > Edit
on this zone and nothing else.
EOF
	exit 2
}

while [ $# -gt 0 ]; do
	case "$1" in
	--domain) domain="${2:-}"; shift 2 ;;
	--ip) ip="${2:-}"; shift 2 ;;
	--mail-host) mail_host="${2:-}"; shift 2 ;;
	--app-host) app_host="${2:-}"; shift 2 ;;
	--dmarc-email) dmarc_email="${2:-}"; shift 2 ;;
	--priority) mx_priority="${2:-}"; shift 2 ;;
	--public) public_pool=1; shift ;;
	--apply) apply=1; shift ;;
	-h | --help) usage ;;
	*) echo "unknown argument: $1" >&2; usage ;;
	esac
done

[ -n "$domain" ] || { echo "--domain is required" >&2; usage; }
[ -n "$ip" ] || { echo "--ip is required" >&2; usage; }
[ -n "${CLOUDFLARE_API_TOKEN:-}" ] || {
	echo "CLOUDFLARE_API_TOKEN is not set." >&2
	echo "Create one at https://dash.cloudflare.com/profile/api-tokens with Zone > DNS > Edit." >&2
	exit 1
}

# A hostname where an address is wanted is the most likely typo here, and it
# fails in a way that looks like a firewall problem rather than a DNS one.
if ! printf '%s' "$ip" | grep -Eq '^[0-9]{1,3}(\.[0-9]{1,3}){3}$'; then
	echo "--ip must be an IPv4 address, got: $ip" >&2
	exit 1
fi

[ -n "$dmarc_email" ] || dmarc_email="dmarc@$domain"

cf() {
	method="$1"
	path="$2"
	shift 2
	curl -sS -X "$method" "$api$path" \
		-H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
		-H "Content-Type: application/json" \
		"$@"
}

# Cloudflare answers with 200 and success:false for most real failures, so the
# HTTP status is not enough to go on.
check() {
	body="$1"
	context="$2"
	if [ "$(printf '%s' "$body" | jq -r '.success')" != "true" ]; then
		echo "cloudflare rejected $context:" >&2
		printf '%s' "$body" | jq -r '.errors[]? | "  \(.code): \(.message)"' >&2
		exit 1
	fi
}

zone_response="$(cf GET "/zones?name=$domain")"
check "$zone_response" "the zone lookup"

zone_id="$(printf '%s' "$zone_response" | jq -r '.result[0].id // empty')"
if [ -z "$zone_id" ]; then
	echo "no zone named $domain on this account." >&2
	echo "Check the spelling, and that the token's scope includes this zone." >&2
	exit 1
fi

echo "zone $domain ($zone_id)"
[ "$apply" -eq 1 ] || echo "dry run — nothing will be changed. Re-run with --apply."
echo

changes=0

# upsert creates or updates one record, and reports what it did rather than
# what it intended to do.
#
#   $1 type   $2 name (fqdn)   $3 content   $4 proxied (true|false)
#   $5 extra JSON merged into the body, for MX priority
upsert() {
	rtype="$1"
	name="$2"
	content="$3"
	proxied="$4"
	extra="${5:-{\}}"

	existing="$(cf GET "/zones/$zone_id/dns_records?type=$rtype&name=$name")"
	check "$existing" "the lookup for $rtype $name"

	# Read back with an explicit emptiness test rather than jq's `//`, which
	# treats `false` as absent. `proxied: false` is the value that matters most
	# here, and reading it as empty makes every record look changed — the
	# script would rewrite all of them on every run and never converge.
	fields="$(printf '%s' "$existing" | jq -r '
	  if (.result | length) > 0 then
	    .result[0] | [.id, .content, (.proxied | tostring), ((.priority // "-") | tostring)]
	  else ["", "", "", ""] end | @tsv')"
	# Tab-separated, because TXT content contains spaces.
	IFS=$'\t' read -r record_id current_content current_proxied current_priority <<<"$fields"

	body="$(jq -nc \
		--arg type "$rtype" --arg name "$name" --arg content "$content" \
		--argjson proxied "$proxied" --argjson extra "$extra" \
		'{type: $type, name: $name, content: $content, proxied: $proxied, ttl: 1} + $extra')"

	# An MX whose priority changed is a changed record even when the target is
	# the same, so compare it too rather than reporting the record as correct.
	wanted_priority="$(printf '%s' "$extra" | jq -r '(.priority // "-") | tostring')"

	if [ -n "$record_id" ]; then
		if [ "$current_content" = "$content" ] &&
			[ "$current_proxied" = "$proxied" ] &&
			[ "$current_priority" = "$wanted_priority" ]; then
			printf '  ok      %-4s %-40s %s\n' "$rtype" "$name" "$content"
			return 0
		fi
		changes=$((changes + 1))
		printf '  update  %-4s %-40s %s\n' "$rtype" "$name" "$content"
		printf '          was: %s (proxied: %s)\n' "$current_content" "$current_proxied"
		[ "$apply" -eq 1 ] || return 0
		result="$(cf PATCH "/zones/$zone_id/dns_records/$record_id" --data "$body")"
		check "$result" "the update of $rtype $name"
	else
		changes=$((changes + 1))
		printf '  create  %-4s %-40s %s\n' "$rtype" "$name" "$content"
		[ "$apply" -eq 1 ] || return 0
		result="$(cf POST "/zones/$zone_id/dns_records" --data "$body")"
		check "$result" "the creation of $rtype $name"
	fi
}

mail_fqdn="$mail_host.$domain"
app_fqdn="$app_host.$domain"

# The mail host. Never proxied: Cloudflare does not carry SMTP through the
# proxy, and an orange cloud here means mail silently stops arriving.
upsert A "$mail_fqdn" "$ip" false

# Points at the A record above, never at an address. An MX whose value is an IP
# literal is invalid, and several large senders enforce that.
upsert MX "$domain" "$mail_fqdn" false "$(jq -nc --argjson p "$mx_priority" '{priority: $p}')"

# Nothing is authorised to send as this domain, because nothing sends as this
# domain. A receive-only domain that publishes no SPF is a free identity for
# anyone who wants one.
upsert TXT "$domain" "v=spf1 -all" false
upsert TXT "_dmarc.$domain" "v=DMARC1; p=reject; rua=mailto:$dmarc_email" false

# Public-pool domains host mail and nothing else. The inbox app lives on one
# hostname regardless of how many domains hand out addresses.
if [ "$public_pool" -eq 0 ]; then
	# The only record here that may be proxied. Left DNS-only anyway: turning
	# the orange cloud on is a click, and a script that quietly enables the
	# proxy on a host you have not yet pointed anywhere is a worse default.
	upsert A "$app_fqdn" "$ip" false
fi

echo
if [ "$changes" -eq 0 ]; then
	echo "everything already matches."
elif [ "$apply" -eq 1 ]; then
	echo "$changes record(s) changed."
else
	echo "$changes record(s) would change. Re-run with --apply."
fi

cat <<EOF

Two things this script cannot do for you:

  PTR   Set reverse DNS for $ip to $mail_fqdn in your host's control panel.
        Reverse DNS is delegated by whoever owns the IP block, not by
        Cloudflare. Senders check it, and a missing PTR is the single most
        commonly missed requirement.

  25    Confirm inbound TCP port 25 actually reaches $ip:

          go run ./tools/smtpsink -addr :25 -dir ./inbox -hostname $mail_fqdn

        Then send it mail from Gmail and from Outlook. Two .eml files on disk
        is the only evidence that counts.
EOF
