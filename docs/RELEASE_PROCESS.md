# Release process

This describes how to cut a StudioForge release, including the very first public alpha,
`v0.1.0-alpha.1`. There is no prior public release and no existing git tags; this document describes
the process to follow going forward, not a history of past releases.

## Versioning

StudioForge follows [Semantic Versioning](https://semver.org/). Pre-release versions use the
`vMAJOR.MINOR.PATCH-alpha.N` naming scheme (later `-beta.N`, `-rc.N` if needed), for example
`v0.1.0-alpha.1`, then `v0.1.0-alpha.2`. Increment `N` for each subsequent alpha build; move to a
`beta` only once the "Current alpha stabilization" items in `docs/ROADMAP.md` are substantially
addressed and the API/config surface is not expected to keep changing release to release. Cutting the
first stable `v0.1.0` is a deliberate decision, not an automatic step after some number of alphas.

## Pre-release checklist

Run these, in order, from a clean working tree on the branch that will become the release:

1. Frontend and Go checks (the same commands `scripts/test.sh`/`scripts/test.ps1` run):
   - `cd web && npm ci`
   - `npm run check`
   - `npm run lint`
   - `npm test`
   - `npm run build`
   - `npm run test:e2e`
   - back at the repository root: `gofmt -l cmd internal testdata` (must print nothing)
   - `go vet ./...`
   - `go test ./...`
2. Race detector: `go test -race ./...`. This needs CGO and does not run on a Windows development
   machine without a C toolchain installed; run it on Linux or macOS, or rely on the `race` job in
   `.github/workflows/ci.yml` (`ubuntu-latest`, Go 1.26.x), which runs it on every push/PR.
3. Vulnerability scan: `go install golang.org/x/vuln/cmd/govulncheck@latest` then
   `govulncheck ./...`. Confirm there are zero reachable vulnerabilities before tagging; if there are,
   raise the `go.mod` floor or update the affected dependency first and re-run the scan.
4. Optional real-environment smoke tests. These are opt-in because they need a billable Claude account
   and/or a running Roblox Studio, and default `go test ./...` never runs them:
   - `STUDIOFORGE_REAL_CLAUDE=1 go test ./...` exercises the real `claude` CLI path end to end.
   - `STUDIOFORGE_REAL_STUDIO=1 go test ./...` exercises the real Roblox Studio MCP launcher path.
   Run both by hand at least once per release when the environment is available, and record the
   actual result (pass, skipped, or not run and why) in the release notes' verification list — never
   claim a result that was not actually observed.
5. Update `CHANGELOG.md`: move any accumulated `[Unreleased]` entries into the version section being
   cut, or fill in the pending `[MAJOR.MINOR.PATCH-alpha.N]` section, keeping the Keep a Changelog
   subsection order (Added, Changed, Fixed, Security, Known limitations).
6. Update `docs/KNOWN_LIMITATIONS.md` and `docs/ROADMAP.md` if the release changed what is wired,
   unwired, or planned.
7. Build the release binaries and archives: `./scripts/package.ps1` on Windows or
   `./scripts/package.sh` on macOS/Linux. Each script first runs the corresponding `build.ps1`/
   `build.sh`, which cross-compiles both `windows-amd64` and `darwin-arm64` (CGO disabled, so either
   host can build both targets), stages `README.md` and `LICENSE` alongside each binary, zips them
   into `artifacts/StudioForge-<version>-windows-amd64.zip` and
   `artifacts/StudioForge-<version>-macos-arm64.zip`, and writes `artifacts/SHA256SUMS.txt`.
8. Verify `artifacts/SHA256SUMS.txt` before uploading or trusting anything built: recompute each
   archive's hash (`Get-FileHash -Algorithm SHA256` on Windows, `shasum -a 256` on macOS/Linux) and
   confirm it matches the corresponding line in the file.
9. Smoke-test the packaged binary itself, not just the source build:
   - Windows: expand the zip and run `./studioforge.exe --mock --no-open`; confirm it starts, and
     check that `./studioforge.exe --version` reports the expected version, commit, and build date.
   - macOS: expand the zip, open `StudioForge.app` (Control-click → Open once, since the build is
     unsigned — see Signing below), and confirm the mock demo loads.
10. Create an annotated tag matching the version and push it, for example:
    ```
    git tag -a v0.1.0-alpha.1 -m "v0.1.0-alpha.1"
    git push origin v0.1.0-alpha.1
    ```
11. Watch the tag-triggered release workflow run, verify its uploaded artifacts and checksums match
    what was smoke-tested locally, then publish the release notes (see below).

## Version, commit, and build date injection

`internal/config` declares build-time defaults of `Version = "dev"`, `Commit = "none"`, and
`BuildDate = "unknown"`. `scripts/build.ps1`/`scripts/build.sh` override them via `-ldflags`:

```
-X github.com/10kkyvl/studioforge/internal/config.Version=<git describe>
-X github.com/10kkyvl/studioforge/internal/config.Commit=<12-char short commit SHA>
-X github.com/10kkyvl/studioforge/internal/config.BuildDate=<UTC build timestamp>
```

`<git describe>` comes from `git describe --tags --always --dirty`, falling back to `dev` if that
fails (for example, no tags are reachable, or the command runs outside a git checkout). The commit
falls back to `none` under the same condition. If the checkout is dirty, the version string carries a
`-dirty` suffix — never tag or publish a release built from a dirty working tree. A binary built by
invoking `go build` directly, bypassing the scripts, keeps the `dev`/`none`/`unknown` defaults; that
is expected for local development builds and is never acceptable for a published release artifact.

## What the tag-triggered release workflow does

`.github/workflows/release.yml` triggers on any pushed tag matching `v*`:

- `package-windows` runs on `windows-latest`, checks out full history (`fetch-depth: 0`, so
  `git describe` can see the tag), sets up Node 22 and Go 1.26.x, runs `./scripts/package.ps1`, and
  uploads everything under `artifacts/` as the `release-artifacts` build artifact. Only this one
  runner builds both platform archives — `CGO_ENABLED=0` makes cross-compiling `darwin/arm64` from
  Windows possible without a macOS host.
- `publish` runs on `ubuntu-latest`, depends on `package-windows`, downloads `release-artifacts`, and
  calls `softprops/action-gh-release` with `generate_release_notes: true`. That creates the GitHub
  release and attaches every file under `artifacts/`, including `SHA256SUMS.txt`.
- The workflow does not pass a custom release body, so GitHub's auto-generated notes (derived from
  commits/PRs) are what the release shows until someone edits it — see "Publishing release notes."

## Signing and notarization

Both packages `scripts/package.ps1`/`scripts/package.sh` produce today are **unsigned development
builds**:

- The Windows zip is not Authenticode-signed; Windows SmartScreen may warn on first run.
- The macOS `.app` is not signed or notarized; Gatekeeper blocks a plain double-click. The documented
  workaround is a one-time Control-click → Open in Finder for that specific app.

Never advise disabling SmartScreen, Gatekeeper, or any other OS protection globally to work around
this. The documented per-app bypass above is the only acceptable guidance until a maintainer holds a
valid Windows Authenticode certificate and an Apple Developer ID for notarization. Once those
credentials exist, add signing/notarization steps to the packaging scripts (or a dedicated CI job)
before this section is updated to describe signed releases.

## Publishing release notes

After the release workflow finishes:

1. Confirm the GitHub release exists with both archives and `SHA256SUMS.txt` attached.
2. Replace or augment the auto-generated notes with the matching `CHANGELOG.md` section (Added,
   Changed, Fixed, Security, Known limitations) so the release reads as a changelog entry rather than
   a raw commit list.
3. For an alpha release, link or paste in the repository's `RELEASE_NOTES.md`, which carries the
   fuller "what works / what doesn't / who this is for" framing that does not belong in a changelog
   entry.
4. Mark the GitHub release as a **pre-release** (the checkbox in the release UI, or
   `gh release edit <tag> --prerelease`) for every `-alpha.N`/`-beta.N`/`-rc.N` build. Only leave it
   unchecked for an actual stable cut.

## Rollback guidance

- **A tag was pushed by mistake, or before the checklist above actually passed:** delete the remote
  tag, and delete the GitHub release if one was already created from it, then fix the underlying issue
  and re-tag:
  ```
  git tag -d v0.1.0-alpha.1
  git push origin :refs/tags/v0.1.0-alpha.1
  ```
  Only do this for a tag nobody has relied on yet. Once people may already have pulled a tag, prefer
  cutting a new `-alpha.N+1` over deleting and rewriting one that already shipped.
- **A published alpha turns out to have a serious problem after people have already downloaded it:**
  mark the existing GitHub release as a pre-release if it was not already, and publish a fixed
  `-alpha.N+1` release rather than deleting the old one out from under anyone who has it. Shipping a
  fix forward is less disruptive than deleting a release or tag others may reference.
- **This process governs StudioForge's own release tags only.** It has nothing to do with the
  per-run git checkpoints that `internal/gitcheckpoint` creates in a *user's own* Roblox project
  repository before every non-plan Claude run. Those checkpoints are ordinary git commits in the
  user's project — rolling one back is a normal `git reset`/`git revert`/`git checkout` performed by
  the user in their own repository, unrelated to StudioForge's own tagging or release infrastructure.

---

# Руководство по релизу (Русский)

1. Запустите полный test script и race detector.
2. Обновите Status, Known Limitations и Final Report фактическими доказательствами.
3. Создайте чистый tag, например `v0.1.0`.
4. Запустите `./scripts/package.ps1` в Windows или `./scripts/package.sh` в macOS.
5. Проверьте каждую запись в `artifacts/SHA256SUMS.txt`.
6. Выполните smoke Windows zip командой `studioforge.exe --mock --no-open`.
7. Выполните smoke `.app` на Apple Silicon, прежде чем заявлять hardware verification.
8. Подпишите/notarize production-релиз macOS и Authenticode-подпишите Windows-релиз при наличии действующих идентификационных данных. Никогда не советуйте пользователю глобально отключать защиту ОС.

Версия, commit и UTC-время сборки внедряются через Go `-ldflags`. Release CI запускается только для tag и загружает archives/checksums в GitHub Releases.
