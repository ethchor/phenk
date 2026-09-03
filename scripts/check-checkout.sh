#!/usr/bin/env bash
#
# Fails when a file the build needs exists on disk but is not in the repository.
#
# This check exists because of a real failure. A blanket `*.eml` rule in
# .gitignore silently excluded every golden MIME fixture under testdata/. The
# tests passed locally, because the files were sitting in the working tree, and
# failed in CI, because a clean checkout does not have them. An earlier bare
# `phenk` rule had already done the same thing to cmd/phenk/.
#
# Everything else in the build runs against the working tree, so nothing else
# can notice the difference between "committed" and "merely present". This runs
# in CI too, which means preflight replays it, so the gap closes at the point
# where it is cheap to fix rather than after a red pipeline.

set -euo pipefail

# Paths that are genuinely generated or local, and are meant to be absent from a
# clean checkout. Anything not matched here has to be tracked.
ALLOWED_PREFIXES=(
  ".git/"
  "bin/"
  "dist/"
  "data/"
  "inbox/"
  "node_modules/"
  "internal/web/dist/"
  "site/.next/"
  "web/.next/"
  ".env"
)

ALLOWED_SUFFIXES=(
  ".tsbuildinfo"
  ".log"
)

cd "$(dirname "$0")/.."

# Files on disk that git does not track, ignored ones included. A file that is
# ignored is exactly as absent from a clean checkout as one that was never
# added, so both are listed.
untracked="$(git ls-files --others --directory --no-empty-directory --exclude-standard)"
ignored="$(git ls-files --others --ignored --exclude-standard)"

problems=()
while IFS= read -r path; do
  [ -z "$path" ] && continue

  allowed=false
  for prefix in "${ALLOWED_PREFIXES[@]}"; do
    case "$path" in "$prefix"*) allowed=true; break ;; esac
  done
  if [ "$allowed" = false ]; then
    for suffix in "${ALLOWED_SUFFIXES[@]}"; do
      case "$path" in *"$suffix") allowed=true; break ;; esac
    done
  fi

  [ "$allowed" = false ] && problems+=("$path")
done <<< "$untracked
$ignored"

if [ ${#problems[@]} -gt 0 ]; then
  echo "These files are in the working tree but not in the repository:"
  printf '  %s\n' "${problems[@]}"
  echo
  echo "A clean checkout will not have them, so CI will build something different"
  echo "from what you just tested. Either commit them, or add them to"
  echo "ALLOWED_PREFIXES in scripts/check-checkout.sh if they are genuinely"
  echo "generated."
  exit 1
fi

echo "the working tree matches the repository"
