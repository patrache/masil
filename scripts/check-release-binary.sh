#!/bin/sh
# Checks a goreleaser-built masil binary before it ships (plan 06 Review Focus 2).
#
#   sh scripts/check-release-binary.sh dist/masil_linux_amd64_v1/masil "$MASIL_DEFAULT_SERVER"
#
# Passes only when
#   1. the expected default server is an https:// URL,
#   2. that exact URL is embedded in the binary (the -X value of
#      github.com/patrache/masil/internal/config.DefaultServer), and
#   3. `go version -m` reports CGO_ENABLED=0.
#
# Do NOT check the default server with `go version -m <bin> | grep DefaultServer=`:
# .goreleaser.yaml builds with -trimpath, and with -trimpath the go command leaves
# -ldflags out of the embedded build info (go.dev/issue/52372), so that grep never
# matches. CGO_ENABLED is still recorded, so step 3 uses build info.
#
# Exit codes: 0 ok, 1 check failed, 2 usage error.

set -eu

die() { printf 'check-release-binary: %s\n' "$*" >&2; exit 1; }

if [ "$#" -ne 2 ]; then
	printf 'usage: %s <masil binary> <expected https default server>\n' "$0" >&2
	exit 2
fi
bin=$1
want=$2

case "$want" in
	https://?*) ;;
	*) die "expected default server must be an https:// URL (got '$want'); set MASIL_DEFAULT_SERVER" ;;
esac
[ -f "$bin" ] || die "no such binary: $bin"

if ! LC_ALL=C grep -a -q -F -e "$want" "$bin"; then
	die "$bin does not embed default server $want (was MASIL_DEFAULT_SERVER set for the build?)"
fi

go=${GO:-go}
command -v "$go" >/dev/null 2>&1 || die "go command not found (needed for go version -m)"
info=$("$go" version -m "$bin") || die "go version -m failed on $bin"
if ! printf '%s\n' "$info" | grep -q -E '^[[:space:]]*build[[:space:]]+CGO_ENABLED=0$'; then
	die "$bin was not built with CGO_ENABLED=0"
fi

printf 'ok: %s embeds default server %s, CGO_ENABLED=0\n' "$bin" "$want"
