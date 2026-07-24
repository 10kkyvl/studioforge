#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
for tool in go node npm; do command -v "$tool" >/dev/null 2>&1 || { echo "$tool is required but was not found on PATH" >&2; exit 1; }; done
(cd "$ROOT/web" && npm ci && npm run build)
# Flags are passed straight through. Add --mock for the seeded demo that needs no
# Claude, Roblox Studio, or Rojo; omit it to run against real projects.
exec go run "$ROOT/cmd/studioforge" "$@"
