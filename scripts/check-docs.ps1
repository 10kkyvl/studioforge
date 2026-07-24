$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
  $files = @()
  foreach ($name in @('README.md', 'README.ru.md', 'CONTRIBUTING.md', 'RELEASE_NOTES.md')) {
    if (Test-Path -LiteralPath $name -PathType Leaf) { $files += (Get-Item -LiteralPath $name) }
  }
  if (Test-Path -LiteralPath 'docs') { $files += Get-ChildItem -LiteralPath 'docs' -Recurse -File -Filter '*.md' }
  if (Test-Path -LiteralPath '.github/ISSUE_TEMPLATE') { $files += Get-ChildItem -LiteralPath '.github/ISSUE_TEMPLATE' -Recurse -File }
  $tokens = @('<REPOSITORY_URL>', '<RELEASE_URL>', '<DOCUMENTATION_URL>', '<DEMO_URL>', '<PLACEHOLDER>', 'v0.1.0-alpha.1', 'StudioForge is an alpha', 'no prior public release and no git tags', 'public alpha')
  $found = $false
  foreach ($file in $files) {
    $relative = Resolve-Path -LiteralPath $file.FullName -Relative
    $relative = $relative -replace '^\.[\\/]', '' -replace '\\', '/'
    $lines = Get-Content -LiteralPath $file.FullName
    for ($i = 0; $i -lt $lines.Count; $i++) {
      foreach ($token in $tokens) {
        if ($lines[$i].Contains($token)) {
          Write-Host "${relative}:$($i + 1): $token"
          $found = $true
        }
      }
    }
  }
  if ($found) { exit 1 }
  Write-Host 'check-docs.ps1: no forbidden tokens found in documentation'
} finally { Pop-Location }
