$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
function Invoke-Checked {
  param([scriptblock]$Action, [string]$Description)
  & $Action
  if ($LASTEXITCODE -ne 0) { throw "$Description failed with exit code $LASTEXITCODE" }
}
foreach ($tool in @('go', 'node', 'npm')) {
  if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool is required but was not found on PATH" }
}
Push-Location (Join-Path $root 'web')
try {
  Invoke-Checked { npm ci } 'npm ci'
  Invoke-Checked { npm run check } 'frontend type check'
  Invoke-Checked { npm run lint } 'frontend lint'
  Invoke-Checked { npm test } 'frontend unit tests'
  Invoke-Checked { npm run build } 'frontend production build'
  Invoke-Checked { npm run test:e2e } 'frontend E2E tests'
} finally { Pop-Location }
Push-Location $root
try {
  $unformatted = gofmt -l cmd internal testdata
  if ($unformatted) { throw "Go files need gofmt: $unformatted" }
  Invoke-Checked { go vet ./... } 'go vet'
  Invoke-Checked { go test ./... } 'go tests'
} finally { Pop-Location }
