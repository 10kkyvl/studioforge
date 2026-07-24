#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
for tool in unzip shasum git; do command -v "$tool" >/dev/null 2>&1 || { echo "verify-artifacts.sh: $tool is required but was not found on PATH" >&2; exit 1; }; done
EXPECTED_VERSION="${1:-}"
if [ -n "$EXPECTED_VERSION" ]; then
  SAFE=$(printf %s "$EXPECTED_VERSION" | tr -c 'A-Za-z0-9._-' '-')
else
  VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
  SAFE=$(printf %s "$VERSION" | tr -c 'A-Za-z0-9._-' '-')
fi
ARTIFACTS="$ROOT/artifacts"
WIN_ZIP="$ARTIFACTS/StudioForge-$SAFE-windows-amd64.zip"
MAC_ZIP="$ARTIFACTS/StudioForge-$SAFE-macos-arm64.zip"
SUMS="$ARTIFACTS/SHA256SUMS.txt"
for f in "$WIN_ZIP" "$MAC_ZIP" "$SUMS"; do
  [ -f "$f" ] || { echo "verify-artifacts.sh: $f: expected file to exist, got missing" >&2; exit 1; }
done
CHECK_LINES=$(grep -F -e "  $(basename "$WIN_ZIP")" -e "  $(basename "$MAC_ZIP")" "$SUMS")
if [ "$(printf '%s\n' "$CHECK_LINES" | wc -l)" -ne 2 ]; then
  echo "verify-artifacts.sh: $SUMS: expected checksum lines for both archives, got: $CHECK_LINES" >&2
  exit 1
fi
if ! (cd "$ARTIFACTS" && printf '%s\n' "$CHECK_LINES" | shasum -a 256 -c - >/dev/null); then
  echo "verify-artifacts.sh: $SUMS: checksum verification failed for one or more archives" >&2
  exit 1
fi
WIN_ENTRIES=$(unzip -Z1 "$WIN_ZIP")
for name in studioforge.exe README.md LICENSE; do
  printf '%s\n' "$WIN_ENTRIES" | grep -Fxq "$name" || { echo "verify-artifacts.sh: $WIN_ZIP: missing expected entry $name" >&2; exit 1; }
done
MAC_ENTRIES=$(unzip -Z1 "$MAC_ZIP")
for name in "StudioForge.app/Contents/MacOS/StudioForge" "StudioForge.app/Contents/Info.plist" "StudioForge.app/Contents/Resources/README.md" "StudioForge.app/Contents/Resources/LICENSE"; do
  printf '%s\n' "$MAC_ENTRIES" | grep -Fxq "$name" || { echo "verify-artifacts.sh: $MAC_ZIP: missing expected entry $name" >&2; exit 1; }
done
EXTRACT_DIR=$(mktemp -d)
trap 'rm -rf "$EXTRACT_DIR"' EXIT
unzip -q "$MAC_ZIP" -d "$EXTRACT_DIR"
PLIST="$EXTRACT_DIR/StudioForge.app/Contents/Info.plist"
[ -f "$PLIST" ] || { echo "verify-artifacts.sh: $MAC_ZIP: Info.plist not found after extraction" >&2; exit 1; }
CFVERSION=$(grep -o '<key>CFBundleShortVersionString</key><string>[^<]*</string>' "$PLIST" | sed -E 's/.*<string>([^<]*)<\/string>/\1/')
if [ -z "$CFVERSION" ]; then
  echo "verify-artifacts.sh: $PLIST: CFBundleShortVersionString is empty" >&2
  exit 1
fi
if [ "$CFVERSION" = "dev" ]; then
  echo "verify-artifacts.sh: $PLIST: CFBundleShortVersionString is 'dev', expected a real release version" >&2
  exit 1
fi
if [ -n "$EXPECTED_VERSION" ]; then
  if [ "$CFVERSION" != "$SAFE" ]; then
    echo "verify-artifacts.sh: $PLIST: expected CFBundleShortVersionString $SAFE, got $CFVERSION" >&2
    exit 1
  fi
  case "$CFVERSION" in
    *-dirty*) echo "verify-artifacts.sh: $PLIST: CFBundleShortVersionString $CFVERSION must not contain -dirty for a tagged release" >&2; exit 1 ;;
  esac
  if printf '%s' "$CFVERSION" | grep -Eq '^[0-9a-f]{7,40}$'; then
    echo "verify-artifacts.sh: $PLIST: CFBundleShortVersionString $CFVERSION looks like a bare commit hash, expected a tagged version" >&2
    exit 1
  fi
fi
if [ "$(uname)" = "Darwin" ]; then
  BIN="$EXTRACT_DIR/StudioForge.app/Contents/MacOS/StudioForge"
  [ -x "$BIN" ] || { echo "verify-artifacts.sh: $BIN: expected executable bit set after extraction, got not executable" >&2; exit 1; }
fi
echo "verify-artifacts.sh: verified $(basename "$WIN_ZIP") and $(basename "$MAC_ZIP")"
