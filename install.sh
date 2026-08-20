#!/bin/sh
# Install the latest dead-drop CLI from GitHub Releases.
#   curl -fsSL https://raw.githubusercontent.com/donkeyx/dead-drop/master/install.sh | sh
#
# Override with PREFIX=/usr/local/bin or VERSION=v0.1.9
# SKIP_ATTEST=1 skips GitHub provenance if gh is installed.
set -eu

REPO="donkeyx/dead-drop"
PREFIX="${PREFIX:-}"
VERSION="${VERSION:-}"

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

os=$(uname -s | tr 'A-Z' 'a-z')
arch=$(uname -m)
case "$os" in
  linux|darwin) ;;
  *) die "unsupported OS $os (linux or darwin). Use: go install github.com/donkeyx/dead-drop/cmd/dead-drop@latest" ;;
esac
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "unsupported arch $arch" ;;
esac

if [ -z "$PREFIX" ]; then
  if [ -w /usr/local/bin ] 2>/dev/null; then
    PREFIX=/usr/local/bin
  else
    PREFIX="${HOME}/.local/bin"
  fi
fi
mkdir -p "$PREFIX"

if [ -z "$VERSION" ]; then
  latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest") || die "could not resolve latest release"
  VERSION=${latest##*/}
fi
case "$VERSION" in
  v*) ;;
  *) VERSION="v${VERSION}" ;;
esac

asset="dead-drop-${os}-${arch}"
base="https://github.com/${REPO}/releases/download/${VERSION}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "installing dead-drop ${VERSION} (${os}/${arch}) -> ${PREFIX}/dead-drop"
curl -fsSL "$base/$asset" -o "$tmp/$asset"
curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$tmp" && grep " $asset\$" SHA256SUMS | sha256sum -c -)
elif command -v shasum >/dev/null 2>&1; then
  want=$(awk -v f="$asset" '$2==f {print $1}' "$tmp/SHA256SUMS")
  [ -n "$want" ] || die "no checksum for $asset"
  got=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
  [ "$got" = "$want" ] || die "checksum mismatch"
else
  die "need sha256sum or shasum to verify the download"
fi

if [ "${SKIP_ATTEST:-}" = "1" ]; then
  say "skip provenance (SKIP_ATTEST=1)"
elif command -v gh >/dev/null 2>&1; then
  say "verifying GitHub attestation"
  gh attestation verify "$tmp/$asset" --repo "$REPO" || die "attestation failed"
else
  say "skip provenance (install GitHub CLI to verify attestations)"
fi

chmod 0755 "$tmp/$asset"
mv "$tmp/$asset" "$PREFIX/dead-drop"
say "ok  $($PREFIX/dead-drop version)"
say "try: dead-drop put -server http://127.0.0.1:8080 -in secret.txt"
