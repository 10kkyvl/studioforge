#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
for tool in go node npm git; do command -v "$tool" >/dev/null 2>&1 || { echo "$tool is required but was not found on PATH" >&2; exit 1; }; done
if [ "${1:-}" != "--skip-frontend" ]; then (cd "$ROOT/web" && npm ci && npm run build); fi
VERSION=$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT=$(git -C "$ROOT" rev-parse --short=12 HEAD 2>/dev/null || echo none)
BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS="-s -w -X github.com/10kkyvl/studioforge/internal/config.Version=$VERSION -X github.com/10kkyvl/studioforge/internal/config.Commit=$COMMIT -X github.com/10kkyvl/studioforge/internal/config.BuildDate=$BUILD_DATE"
mkdir -p "$ROOT/dist/windows-amd64" "$ROOT/dist/darwin-arm64"
(cd "$ROOT" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o dist/windows-amd64/studioforge.exe ./cmd/studioforge)
(cd "$ROOT" && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o dist/darwin-arm64/studioforge ./cmd/studioforge)
echo "Built StudioForge $VERSION for windows/amd64 and darwin/arm64"
