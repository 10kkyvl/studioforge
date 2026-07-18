# GitHub repository metadata (ready to paste)

Values below are drafted for StudioForge's first public alpha. Every URL is a placeholder in the
form `<PLACEHOLDER>` — replace with the real address before publishing; do not guess one.

## Repository description

Use exactly (156 characters, well under GitHub's 350-character limit):

```
Open-source project-level workflow for Claude Code and Roblox Studio, focused on context, orchestration, validation, and repeatable AI-assisted development.
```

Character count: **156**.

## Recommended topics

Lowercase, hyphenated, GitHub topic slugs:

```
roblox
roblox-studio
claude
claude-code
mcp
model-context-protocol
ai-assisted-development
ai-agents
developer-tools
go
golang
sveltekit
rojo
luau
open-source
alpha
workflow-automation
game-development
```

## Homepage field

Leave blank for the first public alpha. There is no hosted documentation site yet. Once one exists,
point the homepage field at it; until then, either leave it empty or point it at the repository's
own docs directory, e.g. `<REPOSITORY_URL>/tree/main/docs`.

## Suggested social preview text

For the 1280x640 social preview image (plain text overlay, no invented metrics):

```
StudioForge
An open-source workflow connecting Claude Code and Roblox Studio
Public alpha
```

## Reddit announcement (r/robloxgamedev style)

```
Title: StudioForge — an open-source workflow layer for Claude Code + Roblox Studio (public alpha, feedback wanted)

I've just made StudioForge public as an alpha. It's a local, single-binary daemon with a small
web UI that helps Claude Code (and optionally Codex) work on a Roblox/Rojo project: it registers
your project, runs an agent against it, streams the run's events live, makes a git checkpoint
before every change so you can review or revert it, and can build your place with Rojo and open it
in Studio.

Important: this does not replace or reimplement Roblox Studio's own official MCP tooling. Roblox
Studio ships its own MCP server (Assistant → Manage MCP Servers → Studio as MCP server). StudioForge
detects that official launcher, starts it, and speaks MCP to it — it adds project-level orchestration,
a tool allowlist by permission profile, and a run/event/checkpoint workflow around it. It complements
the official tooling rather than competing with it, and ships no Roblox Studio plugin of its own.

It's genuinely an alpha: some packages in the codebase (project memory, a task dependency graph,
asset review, Rojo live-sync control) are implemented and tested but not yet wired into the running
UI, and the docs say so plainly rather than listing them as features. Studio access is only granted
to a run when exactly one Studio instance is open, by design — that's a real constraint of how
Studio's MCP client state works, not a bug.

Repo: <REPOSITORY_URL>
Docs: <DOCUMENTATION_URL>
Releases: <RELEASE_URL>

Feedback, issues, and pull requests are welcome — especially from anyone running real Rojo projects
against it.
```

## DevForum announcement (more conservative tone)

```
Title: StudioForge (public alpha) — a workflow layer around Claude Code, Codex, and Studio's official MCP tooling

Hi all — I wanted to share an early, public alpha of an open-source tool called StudioForge. It's a
local daemon with a web UI that sits alongside your existing Roblox Studio and Rojo setup.

To be clear about scope: StudioForge does not replace or modify Roblox Studio, and it does not ship
a Studio plugin. Roblox Studio already provides its own official MCP server (enabled via
Assistant → Manage MCP Servers → Studio as MCP server), and StudioForge's role is to detect that
official launcher, start it per run, and connect an AI coding agent (Claude Code, or Codex without
Studio access) to it under a scoped tool allowlist. On top of that it adds a project registry, a
run scheduler, live event streaming, and an automatic git checkpoint before each change so results
are easy to review or revert.

This is a first public alpha, not a finished product. Some parts described in the repository's own
"known limitations" doc are implemented but not yet reachable from the UI, and platform packages
(Windows/macOS) are unsigned development builds for now. Studio access is intentionally refused
whenever more than one Studio instance is open, since the official MCP client has no way for an
external process to pin which instance an agent's connection is bound to.

I'm sharing this early to get feedback from people who actually use Rojo-based workflows day to day.
Repo: <REPOSITORY_URL>
Docs: <DOCUMENTATION_URL>
Releases: <RELEASE_URL>

Happy to answer questions about what does and doesn't work yet.
```

## X/Twitter announcement

Fits within 280 characters:

```
StudioForge is now public: an open-source workflow layer for Claude Code + Roblox Studio's own
official MCP tooling — project registry, live run events, git checkpoints, Rojo build/open.
Public alpha, feedback welcome. <REPOSITORY_URL>
```

(Character count of the body above is close to the limit — trim the placeholder URL length in the
final post if needed; most URL shorteners keep this under 280 including the link.)

## First public release

**Title:**

```
v0.1.0-alpha.1 — first public alpha
```

**Summary:**

```
This is StudioForge's first public release: a single Go binary with an embedded web UI that
registers Roblox/Rojo projects, runs Claude Code or Codex agents against them, streams run events
live, checkpoints changes with git before each run, and can build and open a project in Roblox
Studio through Rojo and Studio's official MCP tooling. Windows (amd64) and macOS (arm64) builds are
provided as unsigned development packages. This release is an alpha: some implemented packages are
not yet wired into the running product, and real end-to-end paths against a live Claude account or
a live Studio session are covered only by opt-in tests, not by default CI. See
docs/KNOWN_LIMITATIONS.md for the full list before relying on any specific behavior.
```
