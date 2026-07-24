#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
for tool in zip shasum; do command -v "$tool" >/dev/null 2>&1 || { echo "$tool is required but was not found on PATH" >&2; exit 1; }; done
"$ROOT/scripts/build.sh"
for f in "$ROOT/dist/windows-amd64/studioforge.exe" "$ROOT/dist/darwin-arm64/studioforge" "$ROOT/README.md" "$ROOT/LICENSE" "$ROOT/packaging/macos/Info.plist"; do
  [ -f "$f" ] || { echo "package.sh: $f: expected file to exist after build, got missing" >&2; exit 1; }
done
VERSION=$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)
SAFE_VERSION=$(printf %s "$VERSION" | tr -c 'A-Za-z0-9._-' '-')
STAGE="$ROOT/dist/package-stage"
ARTIFACTS="$ROOT/artifacts"
rm -rf "$STAGE"
mkdir -p "$STAGE/StudioForge-windows-amd64" "$STAGE/StudioForge.app/Contents/MacOS" "$STAGE/StudioForge.app/Contents/Resources" "$ARTIFACTS"
find "$ARTIFACTS" -maxdepth 1 -type f -exec rm -f {} +
cp "$ROOT/dist/windows-amd64/studioforge.exe" "$ROOT/README.md" "$ROOT/LICENSE" "$STAGE/StudioForge-windows-amd64/"
(cd "$STAGE/StudioForge-windows-amd64" && zip -qr "$ARTIFACTS/StudioForge-$SAFE_VERSION-windows-amd64.zip" .)
cp "$ROOT/dist/darwin-arm64/studioforge" "$STAGE/StudioForge.app/Contents/MacOS/StudioForge"
chmod +x "$STAGE/StudioForge.app/Contents/MacOS/StudioForge"
sed "s/__VERSION__/$SAFE_VERSION/g" "$ROOT/packaging/macos/Info.plist" > "$STAGE/StudioForge.app/Contents/Info.plist"
cp "$ROOT/README.md" "$ROOT/LICENSE" "$STAGE/StudioForge.app/Contents/Resources/"
(cd "$STAGE" && zip -qry "$ARTIFACTS/StudioForge-$SAFE_VERSION-macos-arm64.zip" StudioForge.app)
(cd "$ARTIFACTS" && shasum -a 256 StudioForge-* > SHA256SUMS.txt)
ACTUAL=$(find "$ARTIFACTS" -maxdepth 1 -type f -exec basename {} \; | sort | tr '\n' ' ')
EXPECTED=$(printf '%s\n' "SHA256SUMS.txt" "StudioForge-$SAFE_VERSION-macos-arm64.zip" "StudioForge-$SAFE_VERSION-windows-amd64.zip" | sort | tr '\n' ' ')
if [ "$ACTUAL" != "$EXPECTED" ]; then
  echo "package.sh: $ARTIFACTS: expected exactly [$EXPECTED], got [$ACTUAL]" >&2
  exit 1
fi
echo "Release artifacts written to $ARTIFACTS"
