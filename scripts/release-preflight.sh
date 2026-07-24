#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
for tool in git node; do command -v "$tool" >/dev/null 2>&1 || { echo "release-preflight.sh: $tool is required but was not found on PATH" >&2; exit 1; }; done
VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  VERSION=$(git describe --tags --exact-match HEAD 2>/dev/null || true)
  if [ -z "$VERSION" ]; then
    echo "release-preflight.sh: no version argument given and HEAD has no exact tag; pass a version, e.g. v0.5.0-rc.1" >&2
    exit 1
  fi
fi
VERSION_RE='^v[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)\.[0-9]+)?$'
if ! printf '%s' "$VERSION" | grep -Eq "$VERSION_RE"; then
  echo "release-preflight.sh: version '$VERSION' does not match required pattern $VERSION_RE" >&2
  exit 1
fi
BARE=${VERSION#v}
SAFE=$(printf %s "$VERSION" | tr -c 'A-Za-z0-9._-' '-')
FAILED=0
fail() {
  echo "release-preflight.sh: $1" >&2
  FAILED=1
}
PKG_VERSION=$(node -e "console.log(require('./web/package.json').version)")
if [ "$PKG_VERSION" != "$BARE" ]; then
  fail "web/package.json: expected version $BARE, got $PKG_VERSION"
fi
LOCK_VERSIONS=$(node -e "const p=require('./web/package-lock.json'); console.log(p.version); console.log((p.packages && p.packages['']) ? p.packages[''].version : '')")
LOCK_ROOT=$(printf '%s\n' "$LOCK_VERSIONS" | sed -n 1p)
LOCK_PKG=$(printf '%s\n' "$LOCK_VERSIONS" | sed -n 2p)
if [ "$LOCK_ROOT" != "$BARE" ]; then
  fail "web/package-lock.json: expected root version $BARE, got $LOCK_ROOT"
fi
if [ "$LOCK_PKG" != "$BARE" ]; then
  fail "web/package-lock.json: expected packages[\"\"].version $BARE, got $LOCK_PKG"
fi
RN_FIRST=$(head -n1 RELEASE_NOTES.md)
RN_EXPECTED="# StudioForge $VERSION"
if [ "$RN_FIRST" != "$RN_EXPECTED" ]; then
  fail "RELEASE_NOTES.md: expected first line '$RN_EXPECTED', got '$RN_FIRST'"
fi
CL_FIRST=$(grep '^## \[' CHANGELOG.md | grep -v '^## \[Unreleased\]' | head -n1 || true)
CL_EXPECTED_PREFIX="## [$BARE]"
case "$CL_FIRST" in
  "$CL_EXPECTED_PREFIX"*) ;;
  *) fail "CHANGELOG.md: expected first '## [' line to start with '$CL_EXPECTED_PREFIX', got '$CL_FIRST'" ;;
esac
if [ -d artifacts ] && [ -n "$(find artifacts -maxdepth 1 -type f 2>/dev/null)" ]; then
  EXPECTED_WIN="StudioForge-$SAFE-windows-amd64.zip"
  EXPECTED_MAC="StudioForge-$SAFE-macos-arm64.zip"
  EXPECTED_SUMS="SHA256SUMS.txt"
  [ -f "artifacts/$EXPECTED_WIN" ] || fail "artifacts/$EXPECTED_WIN: expected file to exist, got missing"
  [ -f "artifacts/$EXPECTED_MAC" ] || fail "artifacts/$EXPECTED_MAC: expected file to exist, got missing"
  [ -f "artifacts/$EXPECTED_SUMS" ] || fail "artifacts/$EXPECTED_SUMS: expected file to exist, got missing"
  for f in artifacts/StudioForge-*; do
    [ -e "$f" ] || continue
    base=$(basename "$f")
    if [ "$base" != "$EXPECTED_WIN" ] && [ "$base" != "$EXPECTED_MAC" ]; then
      fail "artifacts/$base: expected only $EXPECTED_WIN and $EXPECTED_MAC to match StudioForge-*, got unexpected extra file"
    fi
  done
fi
if [ "$FAILED" -ne 0 ]; then
  exit 1
fi
echo "release-preflight.sh: all release checks passed for version $VERSION"
