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
#
# Never calls sudo. Everything runs from main at the bottom, so a truncated
# download through `curl | sh` does nothing.

set -eu

REPO_URL="https://github.com/patrache/masil"

say() { printf '%s\n' "$*"; }
die() { printf 'masil 설치 실패: %s\n' "$*" >&2; exit 1; }

detect_os() {
	os=${MASIL_OS:-$(uname -s)}
	case "$os" in
		Darwin | darwin) echo darwin ;;
		Linux | linux) echo linux ;;
		*)
			printf '지원하지 않는 OS: %s — Windows 는 install.ps1 을 쓰세요\n' "$os" >&2
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
			printf '지원하지 않는 아키텍처: %s — amd64(x86_64)·arm64(aarch64)만 지원합니다\n' "$arch" >&2
			exit 1
			;;
	esac
}

# fetch URL DEST — curl (handles file://) or wget; wget has no file:// so copy those directly.
fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$2" "$1" 2>/dev/null
	elif command -v wget >/dev/null 2>&1; then
		case "$1" in
			file://*) cp "${1#file://}" "$2" 2>/dev/null ;;
			*) wget -qO "$2" "$1" ;;
		esac
	else
		die "curl 또는 wget 이 필요합니다"
	fi
}

download() {
	if ! fetch "$1" "$2"; then
		printf '내려받기 실패: %s (레포가 아직 비공개이거나 버전이 없습니다)\n' "$1" >&2
		exit 1
	fi
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		die "sha256sum 또는 shasum 이 필요합니다"
	fi
}

main() {
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
		[ -n "${HOME:-}" ] || die "HOME 이 비어 있습니다 — MASIL_INSTALL_DIR 로 설치 경로를 정하세요"
		dir="$HOME/.local/bin"
	fi
	target="$dir/masil"

	tmp=$(mktemp -d 2>/dev/null || mktemp -d -t masil-install)
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 1' HUP INT TERM

	download "$base/$archive" "$tmp/$archive"
	download "$base/checksums.txt" "$tmp/checksums.txt"

	want=$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/checksums.txt")
	[ -n "$want" ] || die "checksums.txt 에 $archive 항목이 없습니다"
	got=$(sha256_of "$tmp/$archive")
	if [ "$got" != "$want" ]; then
		printf '체크섬 불일치: %s\n' "$archive" >&2
		exit 1
	fi

	mkdir -p "$tmp/x"
	tar -xzf "$tmp/$archive" -C "$tmp/x" || die "압축을 풀 수 없습니다: $archive"
	[ -f "$tmp/x/masil" ] || die "$archive 안에 masil 실행 파일이 없습니다"

	upgrade=false
	if [ -e "$target" ]; then
		upgrade=true
	fi

	mkdir -p "$dir" || die "설치 경로를 만들 수 없습니다: $dir"
	# Install next to the target, then rename over it: the path never holds a
	# half-written binary, and a running daemon keeps its old inode until it
	# notices the replacement and exits for its supervisor to restart it.
	staged="$dir/.masil.install.$$"
	if ! install -m 0755 "$tmp/x/masil" "$staged"; then
		rm -f "$staged"
		die "설치 경로에 쓸 수 없습니다: $dir"
	fi
	if ! mv -f "$staged" "$target"; then
		rm -f "$staged"
		die "설치 경로에 쓸 수 없습니다: $target"
	fi

	say "masil 설치 완료: $target ($archive)"

	case ":${PATH:-}:" in
		*":$dir:"*)
			found=$(command -v masil 2>/dev/null || true)
			if [ -n "$found" ] && [ "$found" != "$target" ]; then
				say "주의: PATH 에서 다른 masil 이 먼저 잡힙니다: $found"
			fi
			;;
		*)
			say "PATH 에 $dir 이(가) 없습니다. 셸 설정 파일(~/.profile, ~/.zshrc 등)에 다음 줄을 추가하세요:"
			say "  export PATH=\"$dir:\$PATH\""
			;;
	esac

	if [ "$upgrade" = true ]; then
		say "알림 데몬을 자동 시작으로 등록해 두었다면 새 버전으로 곧 다시 뜹니다 — 확인: masil daemon status (안 바뀌면 masil daemon install 을 다시 실행)"
	else
		say "다음 단계:"
		say "  masil signup           # 계정 만들기 (이미 있으면 masil login)"
		say "  masil daemon install   # masil 이 꺼져 있어도 데스크톱 알림 받기"
	fi
}

main "$@"
