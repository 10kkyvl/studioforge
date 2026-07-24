# Release checklist

Companion to [RELEASE_PROCESS.md](RELEASE_PROCESS.md). The first section lists what automation
already covers; the second lists the manual checks a maintainer performs before publishing a
release. Check every box or record why an item was skipped — never claim a result that was not
actually observed.

## Automated (CI on every push/PR, release workflow on tag)

- [ ] Docs consistency: `scripts/check-docs.ps1` / `.sh` (CI `docs` job).
- [ ] Frontend: `npm ci`, `npm run check`, `npm run lint`, `npm test`, `npm run build`,
      `npm run test:e2e`, `npm audit --audit-level=high` (CI `frontend` job).
- [ ] Go: gofmt check, `go vet ./...`, `go test ./...` on the OS matrix (CI `go` job).
- [ ] Race detector: `go test -race ./...` on ubuntu (CI `race` job; needs CGO, so it does not run
      on a Windows dev machine without a C toolchain).
- [ ] Vulnerabilities: `govulncheck ./...` (CI `vuln` job).
- [ ] Release preflight version agreement, packaging for windows-amd64 and macos-arm64,
      `SHA256SUMS.txt`, artifact verification, and native `--version` / `--mock --no-open` health
      smoke tests on Windows and macOS runners (release workflow, on tag).

## Manual, before publishing

- [ ] Windows clean-install test: unpack the release ZIP on a machine (or fresh user account) with
      no prior StudioForge data directory, run `StudioForge.exe`, complete first-run setup, create a
      project, run the built-in demo.
- [ ] macOS Gatekeeper test: unpack the macOS ZIP, launch the unsigned app via right-click → Open,
      confirm the Gatekeeper flow documented in the README works as written.
- [ ] Real OpenRouter smoke test: add a real key in Settings, Test connection, run one small real
      task end to end.
- [ ] Real NVIDIA NIM smoke test: same as above with an `nvapi-` key.
- [ ] Real Claude Code smoke test: authenticated `claude` CLI on PATH, one real run
      (`STUDIOFORGE_REAL_CLAUDE=1 go test ./...` covers the provider path; also verify via the UI).
- [ ] Real Roblox Studio MCP test: Studio with the MCP launcher installed, session discovered after
      Refresh, bind to a project, one Studio-touching action
      (`STUDIOFORGE_REAL_STUDIO=1 go test ./...` covers the launcher path).
- [ ] Real Rojo sync test: start live sync on a project, confirm connect and clean stop from the
      chat view.
- [ ] Rollback test on a disposable Git repository: run against a throwaway project with a real
      checkpoint, open the run diff, perform the rollback, verify the safety branch and commit hash
      shown in the UI exist in the repository.
- [ ] Privacy check of screenshots and demo footage: every image and clip shows only `--mock` demo
      data or scrubbed content — no real usernames, absolute personal paths, keys, tokens, emails,
      or private project names (see docs/screenshots/README.md and the DEMO_SCRIPT.md checklist).
- [ ] GitHub social preview: upload `docs/screenshots/social-preview.png` at
      Settings → General → Social preview (manual; not automated).
- [ ] Draft-release artifact review: both ZIP names match the tag, `SHA256SUMS.txt` matches the
      uploaded files, release notes list only changes actually shipped, then publish.
