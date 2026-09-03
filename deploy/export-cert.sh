#!/bin/sh
# Copy the certificate Caddy issued for the mail hostname into a shared volume
# the SMTP listener can read.
#
# Caddy has no directive that writes a certificate to a path of your choosing:
# it keeps them in its own storage, under a directory named after whichever ACME
# issuer succeeded. So something has to bridge the two, and this is the smallest
# thing that does — no extra image, no extra credentials, and nothing that has
# to be right for mail to flow.
#
# It copies only when the content actually differs. That matters: the listener
# decides whether to re-read the certificate by looking at its modification
# time, so a copy every minute would be a re-parse every minute.

set -eu

: "${MAIL_HOSTNAME:?MAIL_HOSTNAME must be set}"

# Overridable so the loop can be exercised outside a container. In the compose
# stack these are where the two volumes are mounted.
src_dir="${CADDY_CERT_DIR:-/caddy-data/caddy/certificates}"
dst_dir="${EXPORT_DIR:-/srv/tls}"
interval="${EXPORT_INTERVAL:-60}"

# The uid the phenk container runs as. The key is copied with no group or world
# access, so it has to be owned by the reader rather than merely readable.
phenk_uid="${PHENK_UID:-10001}"

# To stderr, which is unbuffered. On stdout these would sit in a stdio buffer
# behind Docker's pipe and only appear once enough of them had accumulated,
# which for a loop this quiet could be never.
log() { echo "export-cert: $*" >&2; }

install_pair() {
	crt="$1"
	key="$2"

	if cmp -s "$crt" "$dst_dir/cert.pem" && cmp -s "$key" "$dst_dir/key.pem"; then
		return 0
	fi

	# Written to a temporary name and renamed, so the listener never reads a
	# half-written certificate. It tolerates one, but there is no reason to
	# hand it one.
	cp "$crt" "$dst_dir/.cert.pem.tmp"
	cp "$key" "$dst_dir/.key.pem.tmp"
	chmod 0644 "$dst_dir/.cert.pem.tmp"
	chmod 0600 "$dst_dir/.key.pem.tmp"
	# The key is readable by its owner and nobody else, so ownership is what
	# makes it readable at all. Failing to chown means the listener would find
	# a key it cannot open, which is worth saying out loud rather than leaving
	# to be discovered as a STARTTLS failure.
	if ! chown "$phenk_uid" "$dst_dir/.cert.pem.tmp" "$dst_dir/.key.pem.tmp" 2>/dev/null; then
		log "warning: could not chown to uid $phenk_uid; the listener may not be able to read the key"
	fi
	mv "$dst_dir/.cert.pem.tmp" "$dst_dir/cert.pem"
	mv "$dst_dir/.key.pem.tmp" "$dst_dir/key.pem"

	log "installed certificate for $MAIL_HOSTNAME"
}

announced_missing=0

while :; do
	crt="$(find "$src_dir" -name "$MAIL_HOSTNAME.crt" -print 2>/dev/null | head -n 1 || true)"
	key="$(find "$src_dir" -name "$MAIL_HOSTNAME.key" -print 2>/dev/null | head -n 1 || true)"

	if [ -n "$crt" ] && [ -n "$key" ]; then
		announced_missing=0
		install_pair "$crt" "$key"
	elif [ "$announced_missing" -eq 0 ]; then
		# Said once rather than every minute. Issuance needs the hostname to
		# resolve to this host and port 80 to reach Caddy, and if it never
		# arrives that is a DNS or firewall problem, not one this loop can fix.
		log "no certificate for $MAIL_HOSTNAME yet, waiting"
		announced_missing=1
	fi

	sleep "$interval"
done
