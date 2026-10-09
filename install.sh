#!/bin/sh
# Installs the goship CLI from the latest GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/coffeece/goship/main/install.sh | sh
#
# Environment:
#   GOSHIP_VERSION      release to install, e.g. v0.3.0 (default: latest)
#   GOSHIP_INSTALL_DIR  where to put the binary (default: /usr/local/bin when
#                       writable, else ~/.local/bin)
set -eu

repo="coffeece/goship"

say() { printf '%s\n' "$*" >&2; }
die() { say "install: $*"; exit 1; }

need() {
	command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}
need curl
need tar

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
	linux | darwin) ;;
	mingw* | msys* | cygwin*) die "on Windows, download the zip from https://github.com/$repo/releases/latest" ;;
	*) die "unsupported OS: $os" ;;
esac

arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "unsupported architecture: $arch" ;;
esac

version="${GOSHIP_VERSION:-}"
if [ -z "$version" ]; then
	# The latest release redirects to its tag; no API call, no rate limit.
	version=$(curl -fsSI -o /dev/null -w '%{redirect_url}' "https://github.com/$repo/releases/latest" | sed 's#.*/tag/##')
	[ -n "$version" ] || die "could not find the latest release"
fi
case "$version" in
	v*) ;;
	*) version="v$version" ;;
esac
bare=${version#v}

archive="goship_${bare}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "Downloading goship $version for $os/$arch..."
curl -fsSL -o "$tmp/$archive" "$base/$archive" || die "no build for $os/$arch in $version"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || die "checksums.txt missing from $version"

if command -v sha256sum >/dev/null 2>&1; then
	sum=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
elif command -v shasum >/dev/null 2>&1; then
	sum=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
else
	die "sha256sum or shasum is required to verify the download"
fi
want=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)
[ -n "$want" ] || die "$archive is not in checksums.txt"
[ "$sum" = "$want" ] || die "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" goship

dir="${GOSHIP_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
	if [ -w /usr/local/bin ]; then
		dir=/usr/local/bin
	else
		dir="$HOME/.local/bin"
	fi
fi
mkdir -p "$dir"
install -m 0755 "$tmp/goship" "$dir/goship"

say "Installed $dir/goship"
case ":$PATH:" in
	*":$dir:"*) ;;
	*) say "Add it to your PATH:  export PATH=\"$dir:\$PATH\"" ;;
esac
say "Next: goship login"
