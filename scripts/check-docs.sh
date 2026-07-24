#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
list_files() {
  for f in README.md README.ru.md CONTRIBUTING.md RELEASE_NOTES.md; do
    [ -f "$f" ] && printf '%s\n' "$f"
  done
  [ -d docs ] && find docs -type f -name '*.md'
  [ -d .github/ISSUE_TEMPLATE ] && find .github/ISSUE_TEMPLATE -type f
  return 0
}
found=0
files=$(list_files)
for f in $files; do
  for token in '<REPOSITORY_URL>' '<RELEASE_URL>' '<DOCUMENTATION_URL>' '<DEMO_URL>' '<PLACEHOLDER>' 'v0.1.0-alpha.1' 'StudioForge is an alpha' 'no prior public release and no git tags' 'public alpha'; do
    lines=$(grep -Fn -- "$token" "$f" | cut -d: -f1)
    for ln in $lines; do
      echo "$f:$ln: $token"
      found=1
    done
  done
done
if [ "$found" -eq 1 ]; then
  exit 1
fi
echo "check-docs.sh: no forbidden tokens found in documentation"
