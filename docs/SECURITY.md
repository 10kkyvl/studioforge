# Security model

This is the detailed security model for StudioForge. The root [SECURITY.md](../SECURITY.md) is
the short vulnerability-reporting policy; read this file for how the daemon actually behaves and
why. Every claim below was checked against the code referenced next to it, on a repository with
no prior public release (first tag will be `v0.1.0-alpha.1`).

## Overview and the core assumption

StudioForge is a **local, single-user development tool**. One Go daemon runs on the operator's own
machine, binds a loopback TCP listener, and drives external CLIs (`claude`, `codex`, `rojo`, `git`)
and the official Roblox Studio MCP launcher on the operator's behalf. There is no server-side
multi-tenant component and no remote account system.

The core assumption that follows from this: **localhost is not a trust boundary**. StudioForge does
not defend against another process running as the same OS user account. If malware is already
running as you, it can already read your files, read your Claude/Codex CLI session, and talk to
anything on `127.0.0.1` that your other local software exposes — with or without StudioForge
installed. StudioForge's local protections (bootstrap token, session cookie, Host/Origin checks —
see [Browser/session security](#browsersession-security)) exist to stop a **web page or another
site's script** from driving the daemon through your browser, not to stop same-user malware. Keep
the workstation itself trustworthy; that is the actual boundary.

## What access the tool receives

Enumerated honestly, StudioForge can:

- Read and write files under the canonical root of each **registered project** (see
  [Local file access](#local-file-access)).
- Execute external CLIs already installed by the operator: `claude`, `codex`, `rojo`, and `git`,
  plus the official Roblox Studio MCP launcher (`%LOCALAPPDATA%\Roblox\mcp.bat` on Windows,
  `RobloxStudio.app/Contents/MacOS/StudioMCP` on macOS). On Windows it can also launch the Roblox
  Studio application itself directly (`RobloxStudioBeta.exe -task EditFile -localPlaceFile <path>`)
  from the **Open in Studio** action (`internal/roblox/studio/studio.go`) — a separate thing from
  launching the MCP launcher for tool access.
- Reach Roblox Studio only through that official launcher's MCP JSON-RPC interface, and only for
  Claude runs (`internal/roblox/mcp`). StudioForge contains no Roblox Studio plugin.
- Accept HTTP connections on the loopback listener (or a non-loopback one only if you pass
  `--unsafe-host`), plus whatever network access the external CLIs make on their own — e.g. Claude
  Code and the Codex CLI talking to their own vendor APIs. StudioForge does not add a proxy in front
  of those calls and does not inspect their contents.

It does **not** ship a Roblox Studio plugin, does not reach the Roblox Marketplace or DataStores
directly, and does not open any listener other than the one loopback (or explicitly unsafe) port.

## Local file access

- A project is added by **registering** a directory (`POST /api/v1/projects`, or automatically for
  the three demo projects under `--mock`). Registration calls `projects.Canonical`
  (`internal/projects/pathguard.go`), which makes the path absolute, cleans it, and resolves
  symlinks with `filepath.EvalSymlinks` — so a project root that is itself a symlink, or sits behind
  one, is registered under its real, resolved location, not the symlink path. `PathGuard.Register`
  then requires the resolved path to exist and be a directory.
- The same file defines `PathGuard.Resolve`, which additionally rejects `..` traversal and a
  symlink used to escape back out of an already-registered root (both cases are unit-tested in
  `internal/projects/pathguard_test.go`). Be precise about what this buys you today: **no HTTP
  handler in `internal/api` calls `Resolve`** — StudioForge exposes no endpoint that accepts a
  project-relative file path from the browser, so there is nothing in the live server for that
  per-path guard to gate yet. It is available for a future feature (for example, a file browser)
  that would need it.
- What a run can and cannot touch, in practice: StudioForge itself reads exactly two files from a
  project — `.agent/constitution.yaml` and `.agent/requirements.md` — verbatim, and prepends them to
  the run's system prompt (`internal/projects/context.go`). It also writes a Rojo skeleton on first
  registration if none exists (`internal/projects/scaffold.go`), and builds/writes a place file under
  `.studioforge/` when you open Studio. Beyond that, **the agent subprocess itself runs with the
  full filesystem permissions of the user account that started StudioForge.** The external CLI
  (`claude` or `codex`) is started with its working directory (`cmd.Dir`) set to the project's
  canonical root, but StudioForge does not sandbox, chroot, or otherwise fence that process's own
  file access — whatever the CLI (or a tool it invokes) chooses to read or write, it can, anywhere
  the OS account can reach. Codex additionally applies its own `--sandbox` mode (see
  [Command execution](#command-execution)); Claude Code has no equivalent OS-level sandbox, only its
  own tool-approval gate.

## Roblox Studio access

- **Fail-closed, exactly one instance.** A run is only granted Studio MCP access when exactly one
  Roblox Studio instance is open (or, once a project names a specific place, when exactly one open
  instance holds that project's place — `internal/roblox/mcp/provisioner.go`). The real technical
  cause: Claude Code runs its own MCP client, and `set_active_studio` is state on *that* connection,
  not something StudioForge can steer from outside; the official launcher also accepts no
  instance-selection argument. With several Studios open, StudioForge cannot tell the launcher which
  one an agent's calls should land on, so it refuses access rather than guessing, and the run simply
  continues without Studio.
- **Permission-profile tool allowlist**, enforced by naming the exact tools Claude is allowed to
  auto-approve for the run (`internal/roblox/mcp/config.go`):
  - `read-only`: observation only — `script_read`, `script_search`, `script_grep`,
    `search_game_tree`, `inspect_instance`, `get_studio_state`, `get_console_output`,
    `screen_capture`, `list_roblox_studios`, `set_active_studio`.
  - `workspace-write`: adds tools that change the open place — `multi_edit`, `execute_luau`,
    `generate_mesh`, `generate_material`, `generate_procedural_model`, `insert_asset`,
    `search_asset`, `wait_job_finished`, `start_stop_play`, `subagent`, `skill`,
    `character_navigation`.
  - `danger-full-access`: adds the tools that reach past the open place —
    `upload_image`, `store_image`, `http_get`, `user_keyboard_input`, `user_mouse_input`. These are
    exactly the tools that can publish to the Marketplace, make arbitrary outbound HTTP requests, or
    send synthetic keyboard/mouse input to the desktop, so they are gated behind the highest tier
    only.
  - An unrecognized profile string grants nothing — a typo denies access rather than widening it.
- **Claude runs only.** The Codex adapter has no `--mcp-config` equivalent, so Codex agents cannot
  reach Studio at all, regardless of permission profile.
- **The playtest validation loop is a second, daemon-initiated Studio MCP connection**
  (`internal/roblox/mcp/validator.go`, `Provisioner.Validate`), separate from the connection the
  agent's own `claude` subprocess used — which has already exited by the time validation runs. Be
  precise about what this means for the tool allowlist above: `AllowedTools` governs which tools
  *Claude itself* may call without an interactive prompt; it is not consulted here, because this is
  StudioForge's own Go code calling `start_stop_play`, `get_console_output`, and `screen_capture`
  directly over its own transport, the same way `Provisioner.probe`/`Status` already call
  `get_studio_state` today. The loop is nonetheless scoped down to the same intent as the
  `workspace-write` tier: it only ever runs for a job whose own permission profile is
  `workspace-write` or `danger-full-access`, and only the three tools named above, never
  `user_keyboard_input`/`user_mouse_input`/`upload_image`/`store_image`/`http_get`
  (`danger-full-access`-only tools) and never Studio content edits (`multi_edit`/`execute_luau`). It is
  further gated behind an explicit per-agent opt-in (`validate_after_run`, off by default) and only
  triggers when the run's own Studio grant succeeded, so a `read-only` run, a Codex run, a plan-mode
  run, or an agent that never opted in never causes the daemon to touch Play mode on its own.
- **Real Studio session discovery is a third kind of daemon-initiated Studio MCP connection**
  (`internal/roblox/mcp/sessions.go`, `Provisioner.ListSessions`), run only on explicit request
  (`POST /api/v1/studio/sessions/refresh`) rather than per-run or on a background timer. It calls
  exactly three read-only-tier tools — `list_roblox_studios`, `set_active_studio`,
  `get_studio_state` — never a tool that changes the open place or reaches beyond it, and it is not
  gated by an agent's permission profile at all, because it never runs as part of a Claude or Codex
  run in the first place. Unlike `Provision` and `Validate`, it deliberately does **not** refuse when
  more than one Studio instance is open: showing every open instance is what a listing view is for,
  not an access grant, so the fail-closed-on-ambiguity rule that governs Studio *access* does not
  apply here. A discovered instance is auto-bound to a registered project only when its reported name
  unambiguously matches that project's expected place file name (the same `PlaceName` rule
  `Provision`'s `Target` already matches on), and only when it was not already bound — an existing
  manual **Bind project** choice (`POST /api/v1/studios/{id}/bind`) is never overridden by a later
  refresh. Under `--mock`, the refresh hook is never wired at all, so a mock install cannot spawn a
  real launcher process by clicking it.

## Command execution

- **What gets spawned.** A run execs one external CLI: `claude -p ...` or `codex exec --json ...`,
  in the project's own directory. Separately, StudioForge can exec `rojo build`/`rojo plugin
  install`/`rojo serve`, `git` (for checkpoints and `doctor`), and the Roblox Studio MCP launcher.
  All of these are executables the operator already has installed; StudioForge does not download or
  execute anything it fetches over the network itself.
- **Reduced environment.** Provider subprocesses receive a fixed allowlist of environment variables
  copied from StudioForge's own process, nothing else (`processes.MinimalEnvironment`,
  `internal/processes/supervisor.go`): `PATH`, `PATHEXT`, `HOME`, `USERPROFILE`, `LOCALAPPDATA`,
  `APPDATA`, `TMPDIR`, `TMP`, `TEMP`, `SYSTEMROOT`, `WINDIR`, `COMSPEC`, `CODEX_HOME`,
  `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `SSL_CERT_FILE`, `SSL_CERT_DIR`. In production no extra
  variables are appended to that list — the parameter that allows extras is only used by tests.
- **Permission profile enforcement differs by provider, and is worth being precise about:**
  - Codex: the profile name is passed straight through as Codex's own `--sandbox` flag value
    (`read-only` / `workspace-write` / `danger-full-access`, defaulting to `workspace-write` for
    anything else) alongside `--ask-for-approval never` (`internal/providers/codex/codex.go`).
    Enforcement of that sandbox is Codex's own, not StudioForge's.
  - Claude: the profile maps to Claude Code's `--permission-mode` — `read-only` → `default` (file
    edits are blocked), `workspace-write` → `acceptEdits` (file edits are auto-accepted; any other
    tool call that Claude Code does not separately auto-approve is simply denied, because
    non-interactive mode has no user to prompt), `danger-full-access` → `bypassPermissions`
    (everything is auto-approved, including arbitrary commands Claude chooses to run). Plan mode
    (the chat "Plan" toggle) always forces `--permission-mode plan` regardless of the agent's
    profile. Claude Code enforces this itself; StudioForge applies no additional OS-level sandbox
    around the Claude process on any tier, including `danger-full-access`.
  - In short: **an agent given `workspace-write` or `danger-full-access` can change the operator's
    project files**, and for Claude with `danger-full-access` it can run arbitrary commands the OS
    account is allowed to run. This is expected of an AI coding agent; do not treat any profile name
    as a promise of confinement equivalent to a container or VM.
- **Git checkpoint, a recovery mechanism, not a preventative control.** Before every Claude run whose
  mode is not `plan`, StudioForge stages and commits the project's current working tree with `git
  -C <root> commit` (`internal/gitcheckpoint/gitcheckpoint.go`), using a fixed local identity so it
  works even without a configured Git author. It is entirely best-effort: a project that is not a
  Git repository, or one with nothing to commit, is a silent no-op, and a checkpoint failure never
  blocks the run. This exists so a bad edit can be reverted with ordinary Git commands afterward — it
  does not stop the edit from happening, and it does not run before Codex or mock runs, nor before a
  `restart` of an interrupted/failed run (only the initial `POST /api/v1/runs` path invokes it).
- **Safe mode** (`--safe-mode`) makes `POST /api/v1/runs` refuse to start any run
  (`internal/api/api.go`, error code `safe_mode`), while diagnostics, settings, backups, and
  export/import stay available. One nuance worth stating precisely: safe mode is checked in the
  "create a run" handler only — `POST /api/v1/runs/{id}/restart` does not repeat that check. Treat
  `--safe-mode` as blocking new work started from the chat composer, not as a global kill switch on
  every run-related endpoint.

## Network access

- The listener binds to loopback (`127.0.0.1` by default) unless you pass `--unsafe-host`, in which
  case `config.Options.Normalize` requires it explicitly:
  a non-loopback `--host` without `--unsafe-host` fails to start
  (`internal/config/config.go`). There is no separate remote-authentication layer added when you do
  pass it — **do not expose StudioForge to an untrusted network**; `--unsafe-host` removes the
  loopback restriction and nothing else.
- No wildcard CORS: StudioForge sets no `Access-Control-*` response headers at all, on any route
  (verified: none exist in `internal/api`), so a browser's default same-origin policy is the only
  thing standing between another site and the API — which is why Host/Origin validation
  (below) exists as a second layer for the mutating routes.
- Beyond the daemon's own listener, network access is whatever the external CLIs make on their own —
  Claude Code and the Codex CLI reach their respective vendor APIs using their own authentication;
  StudioForge does not proxy, inspect, or rate-limit those calls.

## Credential handling

- StudioForge does not store an Anthropic API token or OAuth credential anywhere. Claude
  authentication is entirely owned by the `claude` CLI; StudioForge only shells out to it and reads
  `claude auth status`/`--help` output.
- Codex CLI authentication works the same way: StudioForge runs `codex login status` to check it and
  never reads or stores the underlying credential.
- `internal/platform/secretstore.go` defines a `SecretStore` interface with a Windows Credential
  Manager adapter and a macOS Keychain adapter. Today both are stubs — every method on both types
  unconditionally returns "secret store unavailable" — and nothing else in the daemon calls this
  interface. These are adapter boundaries reserved for a future feature, not an active secret store;
  no credential passes through them in this release.
- Secret redaction (`internal/security/redact.go`) matches common patterns — `key=`/`token=`/
  `password=`-style assignments, `sk-ant-...`/`sk-...` API key shapes, `Authorization: Bearer/Basic`
  headers, and PEM private key blocks — and replaces matches with `[REDACTED]`. In this codebase its
  one production call site is the diagnostic bundle writer (`internal/diagnostics/doctor.go`, used by
  `studioforge doctor --bundle` and described further in [Honest gaps](#honest-gaps-for-the-alpha-release)):
  it is applied to the `doctor.json` report written into that zip. It is not wired into StudioForge's
  own `slog` application logs or into persisted run transcripts/events — those are operational
  status text and streamed provider events, not something this pass currently scans. Practically:
  **review a diagnostic bundle, and review anything you copy out of the chat/event history, before
  sharing it** — the bundle's own `README.json` entry already states plainly that secrets,
  environment variables, prompts, and project source are not included in it by design.

## Browser/session security

- On startup, StudioForge generates one cryptographically random bootstrap token
  (`crypto/rand`, 32 bytes, base64 URL-encoded — `internal/api/session.go`) and prints it together
  with the listener URL. The browser is opened at `#bootstrap=<token>` automatically unless
  `--no-open` is set.
- `POST /api/v1/session/bootstrap` exchanges that token, exactly once (`subtle.ConstantTimeCompare`,
  then a `bootstrapUsed` flag), for a session token. The response sets an `HttpOnly`, `SameSite=Strict`
  cookie (`studioforge_session`) with a 24-hour sliding expiry; every valid request extends it.
  A second attempt to exchange the same bootstrap token is rejected.
- Every `/api/` request must present a `Host` header matching the listener's own address exactly
  (case-insensitively); every mutating request (any method other than `GET`/`HEAD`/`OPTIONS`) must
  also present an `Origin` header whose scheme is `http`/`https` and whose host matches the request's
  `Host`. Only `/api/v1/health` and `/api/v1/session/bootstrap` skip the session-cookie check; every
  other API route requires a valid session.
- Response headers include a restrictive Content-Security-Policy (`default-src 'self'`; no inline
  scripts; `object-src 'none'`; `frame-ancestors 'none'`), `X-Content-Type-Options: nosniff`, and
  `Referrer-Policy: no-referrer`.

## Trust assumptions

Stated explicitly, StudioForge trusts:

- **The operator** — the person running the daemon and approving what an agent does.
- **The local OS user account** — anything that account can already do, StudioForge's local
  protections do not add a second gate against (see [Overview](#overview-and-the-core-assumption)).
- **The installed external CLIs and their own authentication** — `claude`, `codex`, `git`, `rojo`,
  and whatever those tools decide to do with the permission profile/sandbox mode they are given.
- **Roblox's official Studio MCP launcher** — StudioForge does not reimplement Studio operations; it
  detects and speaks MCP to Roblox's own launcher.

StudioForge does **not** trust the network: it does not listen beyond loopback by default, adds no
remote-authentication layer if you force it to, and assumes any code on the same OS user account
could reach anything StudioForge or its subprocesses can reach.

## Honest gaps for the alpha release

- Windows and macOS packages are **unsigned development builds**. Expect SmartScreen/Gatekeeper
  prompts; verify the published SHA-256 before running one.
- A Claude run inherits the **operator's own Claude Code configuration** — `CLAUDE.md`, hooks,
  plugins, skills — which is billed to every run and makes behavior depend on the local install.
  `--strict-mcp-config` is only emitted alongside `--mcp-config`, i.e. only when a run was granted
  Studio access; a run without Studio access still inherits the operator's other configured MCP
  servers. Claude Code's `--bare` would isolate a run fully, but it requires `ANTHROPIC_API_KEY` and
  cannot use OAuth/subscription authentication, so StudioForge does not use it.
- **`--max-turns` does not exist in current Claude Code.** StudioForge's capability probe drops the
  flag when `claude --help` does not advertise it, so an agent's configured max-turns limit does not
  bound a Claude run today — only the budget ceiling (`--max-budget-usd`) does.
- **There is no operator-approval gate before a "dangerous" action.** A `Decision` record type,
  resolve endpoint, and review UI existed early in the alpha but were removed — they never had a live
  producer, and the interactive-question feature (`studioforge-question`, see the README) covers the
  one case that mechanism was meant for: an agent pausing mid-run for the operator's input. Neither
  that feature nor anything else in this release pauses a run for approval before an edit, a
  destructive command, or a publish.
- `internal/diagnostics` (the `doctor`/bundle code path) has no automated test coverage in this
  release; its output is not unverified in the sense of being wrong, but it has not been exercised by
  CI the way most of the rest of the daemon has.
- Real end-to-end paths against an actual Claude account or an actual Roblox Studio instance run only
  behind opt-in environment variables (`STUDIOFORGE_REAL_CLAUDE=1`, `STUDIOFORGE_REAL_STUDIO=1`);
  the default `go test ./...` (and CI) exercises fakes only.
- `PathGuard.Resolve`'s traversal/symlink-escape check has no live caller (see
  [Local file access](#local-file-access)) — do not read its unit tests as evidence that a specific
  HTTP endpoint is guarded, because none currently calls it.
- Safe mode's run-blocking check is not repeated on the run-restart endpoint (see
  [Command execution](#command-execution)).

## Safe usage recommendations

- Register a project that is **already version-controlled** (or let StudioForge initialize one)
  before running an agent against it, so the automatic checkpoint and ordinary Git history give you
  something to revert to.
- Start new agents on `read-only` or `workspace-write`, not `danger-full-access`, until you have
  watched a few runs and are comfortable with what the agent does.
- Keep the daemon on the loopback listener. Only pass `--unsafe-host` if you fully understand that it
  removes the loopback restriction with no remote authentication added.
- Review checkpoint commits and diffs after a run (`git log`, `git diff`) rather than assuming a
  permissive profile only did what you asked.
- Keep `claude`, `codex`, `rojo`, and `git` themselves up to date; StudioForge inherits whatever
  security posture those tools have.
- Review a diagnostic bundle (`studioforge doctor --bundle diagnostics.zip`) before sharing it, even
  though it already excludes prompts, environment variables, and project source by design, and
  applies pattern-based redaction on top of that.

## Vulnerability reporting

See the root [SECURITY.md](../SECURITY.md) for the reporting process. In short: do not file a public
issue for an unpatched vulnerability; use GitHub private vulnerability reporting when enabled; and
never include real API keys, OAuth tokens, full environment dumps, private prompts, or your own
project source in a report — a minimal, sanitized proof of concept is enough.
