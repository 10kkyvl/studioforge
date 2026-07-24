$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
& (Join-Path $PSScriptRoot 'build.ps1')
if ($LASTEXITCODE -ne 0) { throw "build.ps1 failed with exit code $LASTEXITCODE" }
foreach ($f in @('dist/windows-amd64/studioforge.exe', 'dist/darwin-arm64/studioforge', 'README.md', 'LICENSE', 'packaging/macos/Info.plist')) {
  $full = Join-Path $root $f
  if (-not (Test-Path -LiteralPath $full -PathType Leaf)) { throw "package.ps1: ${full}: expected file to exist after build, got missing" }
}
$version = (git -C $root describe --tags --always --dirty 2>$null)
if (-not $version) { $version = 'dev' }
$safeVersion = $version -replace '[^A-Za-z0-9._-]', '-'
$stage = Join-Path $root 'dist/package-stage'
$artifacts = Join-Path $root 'artifacts'
if (Test-Path $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
New-Item -ItemType Directory -Force -Path $stage, $artifacts | Out-Null
# A package invocation defines the complete release set. Clear only files in
# the explicitly scoped artifacts directory so stale archives cannot leak into
# SHA256SUMS.txt or a release upload.
Get-ChildItem -LiteralPath $artifacts -File -ErrorAction SilentlyContinue | Remove-Item -Force
$winStage = Join-Path $stage 'StudioForge-windows-amd64'
New-Item -ItemType Directory -Force -Path $winStage | Out-Null
Copy-Item -LiteralPath (Join-Path $root 'dist/windows-amd64/studioforge.exe'), (Join-Path $root 'README.md'), (Join-Path $root 'LICENSE') -Destination $winStage
$winArchive = Join-Path $artifacts "StudioForge-$safeVersion-windows-amd64.zip"
if (Test-Path $winArchive) { Remove-Item -LiteralPath $winArchive -Force }
Compress-Archive -Path (Join-Path $winStage '*') -DestinationPath $winArchive -CompressionLevel Optimal
$app = Join-Path $stage 'StudioForge.app'
$macOS = Join-Path $app 'Contents/MacOS'; $resources = Join-Path $app 'Contents/Resources'
New-Item -ItemType Directory -Force -Path $macOS, $resources | Out-Null
Copy-Item -LiteralPath (Join-Path $root 'dist/darwin-arm64/studioforge') -Destination (Join-Path $macOS 'StudioForge')
$plist = (Get-Content -LiteralPath (Join-Path $root 'packaging/macos/Info.plist') -Raw).Replace('__VERSION__', $safeVersion)
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText((Join-Path $app 'Contents/Info.plist'), $plist, $utf8NoBom)
Copy-Item -LiteralPath (Join-Path $root 'README.md'), (Join-Path $root 'LICENSE') -Destination $resources
$macArchive = Join-Path $artifacts "StudioForge-$safeVersion-macos-arm64.zip"
if (Test-Path $macArchive) { Remove-Item -LiteralPath $macArchive -Force }
Compress-Archive -Path $app -DestinationPath $macArchive -CompressionLevel Optimal
$checksums = Get-ChildItem -LiteralPath $artifacts -File | Where-Object Name -ne 'SHA256SUMS.txt' | Sort-Object Name | ForEach-Object { "{0}  {1}" -f (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash.ToLowerInvariant(), $_.Name }
[System.IO.File]::WriteAllLines((Join-Path $artifacts 'SHA256SUMS.txt'), $checksums, [System.Text.Encoding]::ASCII)
$expectedNames = @('SHA256SUMS.txt', "StudioForge-$safeVersion-macos-arm64.zip", "StudioForge-$safeVersion-windows-amd64.zip") | Sort-Object
$actualNames = Get-ChildItem -LiteralPath $artifacts -File | Select-Object -ExpandProperty Name | Sort-Object
if (@(Compare-Object -ReferenceObject $expectedNames -DifferenceObject $actualNames).Count -ne 0) {
  throw "package.ps1: ${artifacts}: expected exactly [$($expectedNames -join ', ')], got [$($actualNames -join ', ')]"
}
Write-Host "Release artifacts written to $artifacts"
