#!/bin/sh
# masil installer for macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/patrache/masil/main/scripts/install.sh | sh
#
# Environment:
#   MASIL_VERSION        release to install, e.g. 0.1.0 (default: latest)
#   MASIL_INSTALL_DIR    target directory (default: $HOME/.local/bin)
#   MASIL_DOWNLOAD_BASE  where archives and checksums.txt live (file:// allowed, for local tests)
#   MASIL_OS, MASIL_ARCH override `uname -s` / `uname -m` (tests only)
#   MASIL_INSTALL_LANG   ko | en: message language (default: Korean when LC_ALL,
#                        or LANG when LC_ALL is unset, starts with "ko"; else English)
#
# Never calls sudo. Everything runs from main at the bottom, so a truncated
# download through `curl | sh` does nothing.

set -eu

REPO_URL="https://github.com/patrache/masil"

# Messages come in pairs, Korean then English, on one line: say_t KO EN,
# die_t KO EN, $(t KO EN). msg_lang is set first thing in main.
msg_lang=en
detect_lang() {
	case "${MASIL_INSTALL_LANG:-}" in
		ko | en) echo "$MASIL_INSTALL_LANG"; return ;;
	esac
	case "${LC_ALL:-${LANG:-}}" in
		ko*) echo ko ;;
		*) echo en ;;
	esac
}
t() { if [ "$msg_lang" = ko ]; then printf '%s' "$1"; else printf '%s' "$2"; fi; }
say() { printf '%s\n' "$*"; }
say_t() { say "$(t "$1" "$2")"; }
die() { printf '%s: %s\n' "$(t 'masil 설치 실패' 'masil install failed')" "$*" >&2; exit 1; }
die_t() { die "$(t "$1" "$2")"; }

detect_os() {
	os=${MASIL_OS:-$(uname -s)}
	case "$os" in
		Darwin | darwin) echo darwin ;;
		Linux | linux) echo linux ;;
		*)
			say_t "지원하지 않는 OS: $os — Windows 는 install.ps1 을 쓰세요" "Unsupported OS: $os — on Windows use install.ps1" >&2
			exit 1
			;;
	esac
}

detect_arch() {
	arch=${MASIL_ARCH:-$(uname -m)}
	case "$arch" in
		x86_64 | amd64) echo amd64 ;;
		arm64 | aarch64) echo arm64 ;;
		*)
			say_t "지원하지 않는 아키텍처: $arch — amd64(x86_64)·arm64(aarch64)만 지원합니다" "Unsupported architecture: $arch — only amd64 (x86_64) and arm64 (aarch64) are supported" >&2
			exit 1
			;;
	esac
}

# curl_retry_flags prints the retry options this curl understands. --retry
# covers timeouts and HTTP 408/429/500/502/503/504; --retry-all-errors
# (curl 7.71+) also retries connection resets and the short-lived 404 right
# after a release is published. Older curl rejects the unknown option (exit 2),
# so probe it first.
curl_retry_flags() {
	if curl --retry-all-errors --version >/dev/null 2>&1; then
		echo "--retry 5 --retry-all-errors"
	else
		echo "--retry 5"
	fi
}

# fetch URL DEST — curl (handles file://) or wget; wget has no file:// so copy those directly.
# Network downloads retry: GitHub release downloads answered 504 for a while on
# 2026-11-08. A local file:// copy is not retried. wget retries in a loop here
# because busybox wget has no --tries.
fetch() {
	if command -v curl >/dev/null 2>&1; then
		case "$1" in
			file://*) curl -fsSL -o "$2" "$1" 2>/dev/null ;;
			# curl's own error line (e.g. "returned error: 504") stays on stderr.
			# shellcheck disable=SC2046 # split the flag list into words
			*) curl -fsSL $(curl_retry_flags) -o "$2" "$1" ;;
		esac
	elif command -v wget >/dev/null 2>&1; then
		case "$1" in
			file://*) cp "${1#file://}" "$2" 2>/dev/null ;;
			*)
				n=1
				until wget -qO "$2" "$1"; do
					[ "$n" -lt 5 ] || return 1
					sleep "$n"
					n=$((n + 1))
				done
				;;
		esac
	else
		die_t "curl 또는 wget 이 필요합니다" "curl or wget is required"
	fi
}

download() {
	if ! fetch "$1" "$2"; then
		say_t "내려받기 실패: $1" "Download failed: $1" >&2
		say_t "  HTTP 오류(404: 레포가 아직 비공개이거나 그 버전이 없음, 5xx: GitHub 쪽 일시 장애)나 네트워크 문제일 수 있습니다 — 잠시 뒤 다시 실행하세요." "  This may be an HTTP error (404: the repository is still private or that version does not exist, 5xx: a temporary GitHub outage) or a network problem — try again in a moment." >&2
		exit 1
	fi
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		die_t "sha256sum 또는 shasum 이 필요합니다" "sha256sum or shasum is required"
	fi
}

main() {
	msg_lang=$(detect_lang)
	os=$(detect_os)
	arch=$(detect_arch)
	archive="masil_${os}_${arch}.tar.gz"

	version=${MASIL_VERSION:-}
	version=${version#v}
	if [ -n "${MASIL_DOWNLOAD_BASE:-}" ]; then
		base=${MASIL_DOWNLOAD_BASE%/}
	elif [ -n "$version" ]; then
		base="$REPO_URL/releases/download/v$version"
	else
		base="$REPO_URL/releases/latest/download"
	fi

	if [ -n "${MASIL_INSTALL_DIR:-}" ]; then
		dir=$MASIL_INSTALL_DIR
	else
		[ -n "${HOME:-}" ] || die_t "HOME 이 비어 있습니다 — MASIL_INSTALL_DIR 로 설치 경로를 정하세요" "HOME is empty — set the install folder with MASIL_INSTALL_DIR"
		dir="$HOME/.local/bin"
	fi
	target="$dir/masil"

	tmp=$(mktemp -d 2>/dev/null || mktemp -d -t masil-install)
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 1' HUP INT TERM

	download "$base/$archive" "$tmp/$archive"
	download "$base/checksums.txt" "$tmp/checksums.txt"

	want=$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/checksums.txt")
	[ -n "$want" ] || die_t "checksums.txt 에 $archive 항목이 없습니다" "checksums.txt has no entry for $archive"
	got=$(sha256_of "$tmp/$archive")
	if [ "$got" != "$want" ]; then
		say_t "체크섬 불일치: $archive" "Checksum mismatch: $archive" >&2
		exit 1
	fi

	mkdir -p "$tmp/x"
	tar -xzf "$tmp/$archive" -C "$tmp/x" || die_t "압축을 풀 수 없습니다: $archive" "Cannot extract $archive"
	[ -f "$tmp/x/masil" ] || die_t "$archive 안에 masil 실행 파일이 없습니다" "$archive has no masil executable"

	upgrade=false
	if [ -e "$target" ]; then
		upgrade=true
	fi

	mkdir -p "$dir" || die_t "설치 경로를 만들 수 없습니다: $dir" "Cannot create the install folder: $dir"
	# Install next to the target, then rename over it: the path never holds a
	# half-written binary, and a running daemon keeps its old inode until it
	# notices the replacement and exits for its supervisor to restart it.
	staged="$dir/.masil.install.$$"
	if ! install -m 0755 "$tmp/x/masil" "$staged"; then
		rm -f "$staged"
		die_t "설치 경로에 쓸 수 없습니다: $dir" "Cannot write to the install folder: $dir"
	fi
	if ! mv -f "$staged" "$target"; then
		rm -f "$staged"
		die_t "설치 경로에 쓸 수 없습니다: $target" "Cannot write to the install path: $target"
	fi

	say_t "masil 설치 완료: $target ($archive)" "masil installed: $target ($archive)"

	case ":${PATH:-}:" in
		*":$dir:"*)
			found=$(command -v masil 2>/dev/null || true)
			if [ -n "$found" ] && [ "$found" != "$target" ]; then
				say_t "주의: PATH 에서 다른 masil 이 먼저 잡힙니다: $found" "Note: another masil comes first on your PATH: $found"
			fi
			;;
		*)
			say_t "PATH 에 $dir 이(가) 없습니다. 셸 설정 파일(~/.profile, ~/.zshrc 등)에 다음 줄을 추가하세요:" "$dir is not on your PATH. Add this line to your shell profile (~/.profile, ~/.zshrc, …):"
			say "  export PATH=\"$dir:\$PATH\""
			;;
	esac

	if [ "$upgrade" = true ]; then
		say_t "알림 데몬을 자동 시작으로 등록해 두었다면 새 버전으로 곧 다시 뜹니다 — 확인: masil daemon status (안 바뀌면 masil daemon install 을 다시 실행)" "If the notification daemon is registered to start at login, it restarts on the new version shortly — check: masil daemon status (if it has not changed, run masil daemon install again)"
	else
		say_t "다음 단계:" "Next steps:"
		say_t "  masil signup           # 계정 만들기 (이미 있으면 masil login)" "  masil signup           # create an account (or masil login if you have one)"
		say_t "  masil daemon install   # masil 이 꺼져 있어도 데스크톱 알림 받기" "  masil daemon install   # get desktop notifications even while masil is closed"
	fi
}

main "$@"
