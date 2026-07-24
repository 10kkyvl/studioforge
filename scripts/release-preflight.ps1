$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
  function Write-ScriptError([string]$message) {
    [Console]::Error.WriteLine("release-preflight.ps1: $message")
  }
  $version = $args.Count -gt 0 ? $args[0] : $null
  if (-not $version) {
    $version = (git describe --tags --exact-match HEAD 2>$null)
    if (-not $version) {
      Write-ScriptError 'no version argument given and HEAD has no exact tag; pass a version, e.g. v0.5.0-rc.1'
      exit 1
    }
  }
  $versionPattern = '^v[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)\.[0-9]+)?$'
  if ($version -notmatch $versionPattern) {
    Write-ScriptError "version '$version' does not match required pattern $versionPattern"
    exit 1
  }
  $bare = $version.Substring(1)
  $safe = $version -replace '[^A-Za-z0-9._-]', '-'
  $failed = $false

  $pkg = Get-Content -LiteralPath 'web/package.json' -Raw | ConvertFrom-Json
  if ($pkg.version -ne $bare) {
    Write-ScriptError "web/package.json: expected version $bare, got $($pkg.version)"
    $failed = $true
  }

  $lock = Get-Content -LiteralPath 'web/package-lock.json' -Raw | ConvertFrom-Json -AsHashtable
  $lockRootVersion = $lock['version']
  $lockPkgVersion = $lock['packages']['']['version']
  if ($lockRootVersion -ne $bare) {
    Write-ScriptError "web/package-lock.json: expected root version $bare, got $lockRootVersion"
    $failed = $true
  }
  if ($lockPkgVersion -ne $bare) {
    Write-ScriptError "web/package-lock.json: expected packages[`"`"].version $bare, got $lockPkgVersion"
    $failed = $true
  }

  $rnFirst = Get-Content -LiteralPath 'RELEASE_NOTES.md' -TotalCount 1
  $rnExpected = "# StudioForge $version"
  if ($rnFirst -ne $rnExpected) {
    Write-ScriptError "RELEASE_NOTES.md: expected first line '$rnExpected', got '$rnFirst'"
    $failed = $true
  }

  $clFirst = (Get-Content -LiteralPath 'CHANGELOG.md' | Where-Object { $_.StartsWith('## [') -and -not $_.StartsWith('## [Unreleased]') } | Select-Object -First 1)
  $clExpectedPrefix = "## [$bare]"
  if (-not $clFirst -or -not $clFirst.StartsWith($clExpectedPrefix)) {
    Write-ScriptError "CHANGELOG.md: expected first '## [' line to start with '$clExpectedPrefix', got '$clFirst'"
    $failed = $true
  }

  if ((Test-Path -LiteralPath 'artifacts') -and (Get-ChildItem -LiteralPath 'artifacts' -File -ErrorAction SilentlyContinue)) {
    $expectedWin = "StudioForge-$safe-windows-amd64.zip"
    $expectedMac = "StudioForge-$safe-macos-arm64.zip"
    $expectedSums = 'SHA256SUMS.txt'
    foreach ($name in @($expectedWin, $expectedMac, $expectedSums)) {
      if (-not (Test-Path -LiteralPath (Join-Path 'artifacts' $name))) {
        Write-ScriptError "artifacts/${name}: expected file to exist, got missing"
        $failed = $true
      }
    }
    Get-ChildItem -LiteralPath 'artifacts' -File -Filter 'StudioForge-*' | ForEach-Object {
      if ($_.Name -ne $expectedWin -and $_.Name -ne $expectedMac) {
        Write-ScriptError "artifacts/$($_.Name): expected only $expectedWin and $expectedMac to match StudioForge-*, got unexpected extra file"
        $failed = $true
      }
    }
  }

  if ($failed) { exit 1 }
  Write-Host "release-preflight.ps1: all release checks passed for version $version"
} finally { Pop-Location }
