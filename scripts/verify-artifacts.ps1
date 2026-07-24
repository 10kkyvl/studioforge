$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
  function Write-ScriptError([string]$message) {
    [Console]::Error.WriteLine("verify-artifacts.ps1: $message")
  }
  $expectedVersion = $args.Count -gt 0 ? $args[0] : $null
  if ($expectedVersion) {
    $safe = $expectedVersion -replace '[^A-Za-z0-9._-]', '-'
  } else {
    $version = (git describe --tags --always --dirty 2>$null)
    if (-not $version) { $version = 'dev' }
    $safe = $version -replace '[^A-Za-z0-9._-]', '-'
  }
  $artifacts = Join-Path $root 'artifacts'
  $winZip = Join-Path $artifacts "StudioForge-$safe-windows-amd64.zip"
  $macZip = Join-Path $artifacts "StudioForge-$safe-macos-arm64.zip"
  $sums = Join-Path $artifacts 'SHA256SUMS.txt'
  foreach ($f in @($winZip, $macZip, $sums)) {
    if (-not (Test-Path -LiteralPath $f -PathType Leaf)) {
      Write-ScriptError "${f}: expected file to exist, got missing"
      exit 1
    }
  }
  $sumLines = Get-Content -LiteralPath $sums
  foreach ($zip in @($winZip, $macZip)) {
    $name = Split-Path -Leaf $zip
    $line = $sumLines | Where-Object { $_ -match [regex]::Escape("  $name") } | Select-Object -First 1
    if (-not $line) {
      Write-ScriptError "${sums}: no checksum line found for $name"
      exit 1
    }
    $expectedHash = ($line -split '\s+')[0].ToLowerInvariant()
    $actualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $zip).Hash.ToLowerInvariant()
    if ($expectedHash -ne $actualHash) {
      Write-ScriptError "${sums}: checksum mismatch for $name, expected $expectedHash, got $actualHash"
      exit 1
    }
  }
  Add-Type -AssemblyName System.IO.Compression.FileSystem
  $winArchive = [System.IO.Compression.ZipFile]::OpenRead($winZip)
  try {
    $winEntries = $winArchive.Entries | ForEach-Object { $_.FullName }
    foreach ($name in @('studioforge.exe', 'README.md', 'LICENSE')) {
      if ($winEntries -notcontains $name) {
        Write-ScriptError "${winZip}: missing expected entry $name"
        exit 1
      }
    }
  } finally { $winArchive.Dispose() }
  $macArchive = [System.IO.Compression.ZipFile]::OpenRead($macZip)
  try {
    $macEntries = $macArchive.Entries | ForEach-Object { $_.FullName }
    foreach ($name in @('StudioForge.app/Contents/MacOS/StudioForge', 'StudioForge.app/Contents/Info.plist', 'StudioForge.app/Contents/Resources/README.md', 'StudioForge.app/Contents/Resources/LICENSE')) {
      if ($macEntries -notcontains $name) {
        Write-ScriptError "${macZip}: missing expected entry $name"
        exit 1
      }
    }
  } finally { $macArchive.Dispose() }
  $extractDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
  New-Item -ItemType Directory -Force -Path $extractDir | Out-Null
  try {
    Expand-Archive -LiteralPath $macZip -DestinationPath $extractDir -Force
    $plist = Join-Path $extractDir 'StudioForge.app/Contents/Info.plist'
    if (-not (Test-Path -LiteralPath $plist -PathType Leaf)) {
      Write-ScriptError "${macZip}: Info.plist not found after extraction"
      exit 1
    }
    $plistText = Get-Content -LiteralPath $plist -Raw
    $match = [regex]::Match($plistText, '<key>CFBundleShortVersionString</key><string>([^<]*)</string>')
    $cfVersion = $match.Success ? $match.Groups[1].Value : ''
    if (-not $cfVersion) {
      Write-ScriptError "${plist}: CFBundleShortVersionString is empty"
      exit 1
    }
    if ($cfVersion -eq 'dev') {
      Write-ScriptError "${plist}: CFBundleShortVersionString is 'dev', expected a real release version"
      exit 1
    }
    if ($expectedVersion) {
      if ($cfVersion -ne $safe) {
        Write-ScriptError "${plist}: expected CFBundleShortVersionString $safe, got $cfVersion"
        exit 1
      }
      if ($cfVersion -match '-dirty') {
        Write-ScriptError "${plist}: CFBundleShortVersionString $cfVersion must not contain -dirty for a tagged release"
        exit 1
      }
      if ($cfVersion -match '^[0-9a-f]{7,40}$') {
        Write-ScriptError "${plist}: CFBundleShortVersionString $cfVersion looks like a bare commit hash, expected a tagged version"
        exit 1
      }
    }
  } finally { Remove-Item -LiteralPath $extractDir -Recurse -Force -ErrorAction SilentlyContinue }
  Write-Host "verify-artifacts.ps1: verified $(Split-Path -Leaf $winZip) and $(Split-Path -Leaf $macZip)"
} finally { Pop-Location }
