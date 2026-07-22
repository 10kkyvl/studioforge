# Troubleshooting

Problem -> cause -> fix reference for StudioForge. If you don't find your issue here, check
[Known Limitations](KNOWN_LIMITATIONS.md) first — a surprising number of "bugs" are actually features
that exist in the codebase but are not wired up yet.

---

## Daemon will not start

### "another StudioForge instance is using this data directory"

**Cause:** StudioForge takes an exclusive lock file (`studioforge.lock`) in the data directory on
startup. If another StudioForge process already holds that data directory's lock and is still alive,
a second launch is refused.

**Fix:** Use the already-running instance (check the URL it printed), or stop that process cleanly.
If the previous process crashed instead of exiting cleanly, the lock file is stale; StudioForge detects
that the recorded PID is no longer alive and removes the stale lock automatically on the next start —
you do not need to delete it by hand.

### "listen on HOST:PORT: ..." / port already in use

**Cause:** The requested TCP port is occupied by another process. By default StudioForge picks a free
port automatically (`--port 0`), so this normally only happens if you explicitly passed `--port N` for
a port that's already bound.

**Fix:** Pass a different `--port`, or omit `--port` entirely to let StudioForge choose a free one.

### "non-loopback --host requires --unsafe-host; remote exposure is not recommended"

**Cause:** StudioForge refuses to bind any host other than loopback (`127.0.0.1`, `::1`, or
`localhost`) unless you explicitly opt in.

**Fix:** Only pass `--unsafe-host` if you understand the consequences: StudioForge adds no remote
authentication of its own, so exposing it beyond loopback should not be done on an untrusted network.
See [../SECURITY.md](../SECURITY.md) for the local security model.

---

## Browser opens but you are not authenticated

**Cause:** The bootstrap token printed at startup (`STUDIOFORGE_BOOTSTRAP=...`) is one-use. Once it has
been exchanged for a session cookie, a second tab, a page reload with the same token in the URL
fragment, or reusing a token from a previous run will fail the exchange with 401.

**Fix:** Use the browser tab StudioForge opened for you (it already holds the session cookie), or if
you closed it, restart the daemon and use the fresh `STUDIOFORGE_URL`/token it prints. The session
cookie itself is valid for 24 hours once established, so you don't need to re-bootstrap on every page
load within that window.

---

## `claude` not found

**Cause:** StudioForge resolves the `claude` executable from PATH by default, or from a path you
configured. `studioforge doctor` reports `missing` when the executable cannot be located at all,
`error` when it was found but failed to run, and `warning` when it runs but is not authenticated.

**Fix:**
- Run `studioforge doctor` to see the exact status, detected path, and version.
- Set an explicit executable path in **Settings -> Agents and integrations** (`claude_path`) if PATH
  resolution picks the wrong binary or none at all. This applies immediately, no restart needed.
- Verify independently outside StudioForge: `claude --version` / `claude auth status`.

---

## OpenRouter key or model problems

**Cause:** OpenRouter is an HTTP API, not a CLI, so `studioforge doctor` and the Settings integration
card report on the API key's state and the model catalog's reachability instead of an executable path
and version.

**Fix:**
- **`not_configured`:** no key has been saved and `OPENROUTER_API_KEY` is not set. Add a key in
  **Settings -> Agents and integrations -> OpenRouter**, or set the environment variable and restart
  the daemon. An API key is required to use OpenRouter at all, even for free models.
- **`unverified`:** a key is present but has not been checked (or the last check couldn't reach
  OpenRouter). Click **Test connection** in Settings, or check your network connectivity to
  `openrouter.ai`.
- **`invalid`:** OpenRouter's own `/key` endpoint returned 401 for the stored key. Generate a new key
  on OpenRouter's dashboard and save it again.
- **A run fails immediately with a model/parameter error:** not every OpenRouter model supports tool
  calling, and StudioForge's agent loop is built around tools. The picker shows the catalog's tool,
  vision, context, free, and verification facts; known non-tool models are rejected, while unknown or
  stale IDs require an explicit compatibility confirmation. The backend refreshes the catalog again
  before a run, so a model removed after selection is also rejected. `openrouter/free` remains
  unverified because its eventual model is selected dynamically.
- **A run fails with `openrouter.image_unsupported`:** the attached image was sent to a model that
  either doesn't support vision or whose capabilities aren't known to the catalog. Switch to a
  vision-capable model, or remove the attachment.
- **Free models feel unreliable:** that's expected, not a bug — free models on OpenRouter have more
  variable quality, latency, and rate limits, and availability can change. They suit small tasks better
  than long unattended runs. StudioForge never silently falls back to a paid model when a free-mode run
  hits trouble; it fails the run instead so cost never changes without your say-so.
- **Restart/Resume fails on an old run with a removed-provider error:** that run was saved with
  `provider="codex"` before the Codex CLI provider was removed. It stays fully readable as history (see
  its **Legacy provider** badge) but cannot be restarted or resumed — start a new run instead.

---

## Studio access not granted

**Cause:** Studio access is fail-closed by design: a run is only granted access when exactly one open
Roblox Studio instance holds that project's place file (or, if no project match is configured, when
exactly one Studio instance is open at all). With zero or several instances open, StudioForge refuses
rather than guessing, and the run proceeds without Studio tools. This reflects a real constraint —
Claude Code owns its own MCP connection, `set_active_studio` is per-connection state, and the launcher
accepts no instance-selection argument — not an arbitrary limitation.

**Fix:**
- Check `GET /api/v1/studio-status` (surfaced as the Studio badge in chat) or `studioforge doctor` to
  confirm the launcher is detected at all.
- Leave exactly one relevant Roblox Studio instance open, holding the project's built place
  (`<project>/.studioforge/<name>.rbxl`).
- If StudioForge is configured to open Studio automatically for a project (`studio_auto_open`, on by
  default) and **no Roblox Studio instance is open at all**, it will build and launch it rather than
  fail; give it a few seconds — Studio has to build a window before it can be recognized. Auto-open
  never fires while some other Studio instance is already open, even one that does not hold this
  project — that would risk piling a second window onto Studio rather than the one this project wants.
  The withheld notice in that case names what is actually open next to what StudioForge expected, so a
  project's original/source `.rbxl` opened by hand instead of its built `.studioforge/<name>.rbxl` place
  is easy to recognize.
- **Both Claude and OpenRouter runs are subject to the same fail-closed rule** — neither provider gets
  a wider grant than the other. If a run proceeded without Studio, check the reasons above rather than
  the provider.

---

## Studio MCP launcher not detected

**Cause:** StudioForge looks for Roblox's official launcher at a fixed platform path (or your
configured override):

- Windows: `%LOCALAPPDATA%\Roblox\mcp.bat`
- macOS: `/Applications/RobloxStudio.app/Contents/MacOS/StudioMCP`
- Linux: not supported at all — Roblox Studio itself does not run on Linux, so there is no launcher to
  detect.

**Fix:** Update Roblox Studio to a current version and enable **Assistant -> ... -> Manage MCP Servers
-> Studio as MCP server**. If Studio is installed somewhere non-standard, set the override path in
**Settings** (`studio_mcp_path`) to the launcher executable (or the `.bat`/`.cmd` file on Windows;
StudioForge wraps `.bat`/`.cmd` overrides in `cmd.exe /c` automatically). `studioforge doctor` reports
whether the launcher was found and at what path.

---

## Another MCP client holds the launcher's WS-host slot

**Cause:** The official launcher advertises its full tool list only to whichever process first won its
WebSocket host port; every other client connected to the same launcher is told it has zero tools for as
long as it's connected — even though calls to those tools still succeed through the host. If an agent
started while some other MCP client (another StudioForge run, an editor extension, a second manual
connection) held that slot, a naive client would see an empty tool list and appear unable to use
Studio at all.

**Fix:** This is exactly what StudioForge's stdio shim (`studioforge mcp-shim`, wired into every
generated per-run config automatically — you don't invoke it yourself) exists to paper over: it answers
`tools/list` from whatever it can find, in order of preference — a live response from the launcher,
then a cached tool list saved from an earlier successful connection, then a built-in fallback list with
open (unconstrained) argument schemas — and forwards every `tools/call` straight through to the
launcher untouched. In practice this means an agent's tool list stays populated and its Studio calls
keep working even when it isn't the process holding the host slot. If a run's Studio tools look wrong
(missing argument hints) rather than absent, that is the fallback-schema path — it does not affect
whether the call itself succeeds.

A held host slot also makes the launcher list **zero open instances with no error** — at the listing
level a machine with Studio open looks identical to one with no Studio at all. StudioForge breaks that
tie by checking whether a Studio process is actually running before it ever launches one (both the
provisioner's auto-open and the manual **Open Studio** button): if Studio is running but lists no
instances, the launch is withheld with the host-taken notice above instead of opening a duplicate
window. If you see that notice, close the other MCP client (or disable its Roblox MCP server) and
retry.

---

## Rojo problems

**Cause / fix by symptom:**

- **"no default.project.json in the project; cannot build a place":** the registered project root has
  no `default.project.json`. StudioForge refuses to build rather than guess at a tree — add one at the
  project root.
- **Build fails for another reason:** confirm `rojo --version` works from the same executable path
  StudioForge is configured to use (**Settings -> Rojo path**, or PATH), and that the project file is
  valid Rojo project JSON.
- **Nothing happens when Studio should open automatically:** Studio detection runs before the build; a
  missing Studio install is reported and no place is built.
- **"How do I use live `rojo serve` for live-sync editing?"** — you can't, from StudioForge, today.
  Live-sync session management (`internal/rojo`'s `Manager.Start`/`Stop`/`Session`, per-project port
  allocation, log streaming) exists in the codebase and is unit-tested, but no HTTP endpoint starts,
  stops, or queries a `rojo serve` session. Only Rojo **build + open place** is reachable from the
  product. Run `rojo serve` yourself with the Rojo CLI/VS Code extension if you need live sync; this is
  out of scope for StudioForge issue reports until it is wired up.

---

## Database problems

**Cause:** The database is a single SQLite file at `<data-dir>/studioforge.db`, opened in WAL mode with
a 5-second busy timeout and foreign keys enabled. `studioforge doctor` runs `PRAGMA integrity_check`
and `PRAGMA foreign_key_check` and reports `database: "ok"` or `"error"` (with `wal`/`fts5` booleans).

**Fix:**
- **Locked / busy errors:** these should self-resolve within the 5-second busy timeout under normal
  concurrent access; if they persist, another process may be holding the file outside StudioForge —
  make sure only one StudioForge instance is pointed at that data directory (see the lock-file section
  above).
- **Integrity check failure:** run `studioforge doctor --bundle diagnostics.zip` to capture the report,
  stop the daemon, and restore from a known-good backup. Do not attempt to hand-edit the SQLite file.
- **Backups:** StudioForge creates an automatic backup at most once per 24 hours, and you can trigger
  one manually via the Backups action in Settings. Backups use SQLite's `VACUUM INTO`, land under
  `<data-dir>/backups/`, and are refused if the target file already exists (so a retry never silently
  overwrites a prior backup) — restore only while the daemon is stopped.
- **Where the database lives:** run `studioforge doctor` and read `dataPath`, or pass `--data-dir` at
  startup to control it directly.

---

## Permission / path errors

**Cause:** Every project root is canonicalized (`filepath.Abs` + `filepath.EvalSymlinks`) when
registered, and every file path an adapter touches is re-resolved against that canonical root and
checked for containment before use. A path that resolves outside the registered root — including via a
symlink that points outside it — is rejected with "path is outside the registered project root" rather
than followed.

**Fix:** This is intentional sandboxing, not a bug. If a legitimate path is being rejected, check
whether a symlink in the project tree points outside the registered root, and either remove the symlink
or re-register the project at the directory that actually contains the target files.

---

## Windows vs macOS differences

- **Unsigned packages:** both the Windows zip and the macOS `.app` are unsigned development builds
  until a maintainer supplies signing/notarization credentials. Verify `SHA256SUMS.txt` before running
  either.
- **macOS Gatekeeper:** the first launch of an unsigned `StudioForge.app` requires Control-click ->
  Open in Finder (not a double-click). Do this once per machine; never run commands that globally
  disable Gatekeeper to work around it.
- **PowerShell script execution:** if `./scripts/dev.ps1` or similar fails to run at all (rather than
  failing partway through) with an execution-policy error, your PowerShell execution policy is blocking
  local scripts. Unblock the specific downloaded files (`Unblock-File`) or run with a scoped bypass for
  that one invocation rather than changing the machine-wide policy.

---

## Frontend build issues

### `npm ci` fails with `ENOTEMPTY` on Windows

**Cause:** This was actually hit during release preparation on Windows. `npm ci`'s directory-replacement
step in `web/node_modules` can fail with `ENOTEMPTY` when antivirus software, a file indexer, or a
leftover file handle from a previous run is holding a lock on part of the tree mid-operation.

**Fix:** Delete `web/node_modules` entirely and re-run `npm ci`:

```powershell
Remove-Item -Recurse -Force web/node_modules
cd web; npm ci
```

---

## `go test -race` fails locally on Windows

**Cause:** Go's race detector requires CGO, and this project's default build is `CGO_ENABLED=0` (a
CGO-free, single-binary daemon). Without a configured C toolchain, `-race` cannot be used locally on
Windows.

**Fix:** This is expected — don't chase a CGO toolchain just to run `-race` locally. CI runs
`go test -race ./...` on `ubuntu-latest`, where it works normally; rely on CI for race coverage, and use
plain `go test ./...` (or `./scripts/test.ps1` / `./scripts/test.sh`) locally on Windows.

---

## Stale / interrupted runs

**Cause:** If the daemon exits (crash, forced shutdown) while runs are active, those runs are left in
an `interrupted` state in the database. On the next startup, StudioForge automatically recovers them
(`RecoverInterrupted`) so they don't stay stuck mid-flight indefinitely.

**Fix:** A run left in `interrupted`, `failed`, or `cancelled` status can be restarted from the UI (or
`POST /api/v1/runs/{id}/restart`), which creates a **new** auditable run rather than resuming the old
one in place. Other lifecycle actions are `pause`, `resume`, and `cancel`
(`POST /api/v1/runs/{id}/{action}`): pause/resume are cooperative at event boundaries, and cancel
terminates the provider's process tree. A run in any other status cannot be restarted.

---

## Log collection

To gather diagnostics for a bug report:

```sh
studioforge doctor --bundle diagnostics.zip
```

This writes a zip containing the same JSON report `studioforge doctor` prints (version, OS/arch, data
path, database/WAL/FTS5 status, safe/mock mode, and a per-dependency check for git/claude/rojo/
studioMcp/openrouter) plus a note confirming secrets, environment variables, prompts, and project
source are not included. The OpenRouter check reports only the API key's verification state and
source, never the key itself.

**Redaction is pattern-based, not a guarantee.** The bundle content passes through a redaction filter
that matches known secret shapes — `key=value`/`key: value` pairs named like `api_key`, `token`,
`secret`, or `password`; Anthropic/OpenAI-style `sk-...` API keys; `Authorization: Bearer/Basic ...`
headers; and PEM private key blocks. This catches common cases but is not exhaustive. **Review the
bundle's contents yourself before sharing it**, especially if your data directory or configured paths
contain anything unusual.

---

## See also

- [GETTING_STARTED.md](GETTING_STARTED.md) — installation, first launch, and first workflow.
- [../README.md](../README.md) — project overview.
- [ARCHITECTURE.md](ARCHITECTURE.md) — how the pieces fit together.
- [../SECURITY.md](../SECURITY.md) — local security model and vulnerability reporting.
- [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md) — implemented vs. experimental vs. not-yet-wired.
