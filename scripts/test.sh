#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
for tool in go node npm; do command -v "$tool" >/dev/null 2>&1 || { echo "$tool is required but was not found on PATH" >&2; exit 1; }; done
(cd "$ROOT/web" && npm ci && npm run check && npm run lint && npm test && npm run build && npm run test:e2e)
cd "$ROOT"
UNFORMATTED=$(gofmt -l cmd internal testdata)
[ -z "$UNFORMATTED" ] || { echo "Go files need gofmt: $UNFORMATTED" >&2; exit 1; }
go vet ./...
go test ./...
