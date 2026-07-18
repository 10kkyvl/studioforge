$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
foreach ($tool in @('go', 'node', 'npm')) {
  if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool is required but was not found on PATH" }
}
Push-Location (Join-Path $root 'web')
try {
  npm ci
  npm run build
} finally { Pop-Location }
# Flags are passed straight through. Add --mock for the seeded demo that needs no
# Claude, Roblox Studio, or Rojo; omit it to run against real projects.
go run (Join-Path $root 'cmd/studioforge') @args
