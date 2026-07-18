param([switch]$SkipFrontend)
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
if (-not $SkipFrontend) {
  Push-Location (Join-Path $root 'web')
  try {
    Invoke-Checked { npm ci } 'npm ci'
    Invoke-Checked { npm run build } 'frontend production build'
  } finally { Pop-Location }
}
$version = (git -C $root describe --tags --always --dirty 2>$null)
if (-not $version) { $version = 'dev' }
$commit = (git -C $root rev-parse --short=12 HEAD 2>$null)
if (-not $commit) { $commit = 'none' }
$buildDate = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
$ldflags = "-s -w -X github.com/10kkyvl/studioforge/internal/config.Version=$version -X github.com/10kkyvl/studioforge/internal/config.Commit=$commit -X github.com/10kkyvl/studioforge/internal/config.BuildDate=$buildDate"
$dist = Join-Path $root 'dist'
New-Item -ItemType Directory -Force -Path (Join-Path $dist 'windows-amd64'), (Join-Path $dist 'darwin-arm64') | Out-Null
Push-Location $root
try {
  $env:CGO_ENABLED = '0'; $env:GOOS = 'windows'; $env:GOARCH = 'amd64'
  Invoke-Checked { go build -trimpath -ldflags $ldflags -o (Join-Path $dist 'windows-amd64/studioforge.exe') ./cmd/studioforge } 'Windows build'
  $env:GOOS = 'darwin'; $env:GOARCH = 'arm64'
  Invoke-Checked { go build -trimpath -ldflags $ldflags -o (Join-Path $dist 'darwin-arm64/studioforge') ./cmd/studioforge } 'macOS build'
} finally { Pop-Location }
Write-Host "Built StudioForge $version for windows/amd64 and darwin/arm64"
