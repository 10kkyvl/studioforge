# GitHub repository metadata (ready to paste)

Values below are drafted for StudioForge's current public beta, heading toward the v0.5.0-rc.1
release candidate. Every URL below is real (the repository and its releases page); there is no
hosted docs site or demo site, so none is invented here.

## Repository description

Use exactly (178 characters, well under GitHub's 350-character limit):

```
Open-source desktop app that runs AI coding agents on your Roblox project: live output, diffs, git rollback, task board, and Rojo/Studio live-sync. Public beta, feedback welcome.
```

Character count: **178**.

## Recommended topics

Lowercase, hyphenated, GitHub topic slugs:

```
roblox
roblox-studio
rojo
luau
ai
ai-agents
coding-agent
claude
claude-code
openrouter
developer-tools
go
svelte
game-development
open-source
desktop-app
```

## Homepage field

Either leave it blank, or point it at the [Releases page](https://github.com/10kkyvl/studioforge/releases)
so a visitor lands on downloadable builds. There is no hosted documentation site or demo, so don't
put anything else there.

## Suggested social preview text

For the 1280x640 social preview image (plain text overlay, no invented metrics):

```
StudioForge
AI coding agents for your Roblox project — local, open-source, one project at a time
Public beta · v0.5.0-rc.1
```

## Reddit announcement (r/robloxgamedev style)

```
Title: StudioForge — a local, open-source app that runs AI coding agents on your Roblox project (public beta)

StudioForge is a free, open-source desktop app for Roblox creators. You describe what you want
built in chat, and an AI coding agent writes the code, builds the place, and can test it — inside
your own Roblox Studio, on your own machine.

What it actually does: you create a project pointing at your Roblox/Rojo folder, queue a
prompt-driven run, and watch the agent's output stream live. Every run gets its own git checkpoint
first, so you can review a per-run diff and roll back anything you don't like. There's a task board
with dependencies for planning bigger work, live-sync to Roblox Studio via Rojo, and a Sessions
view that discovers and binds open Studio instances so a run can reach into the one you're
actually using.

Providers: Claude Code (if you already have the CLI installed and signed in), OpenRouter or NVIDIA
NIM (bring your own free-tier API key), or a deterministic --mock demo mode that needs no accounts
at all.

Honest state of things: this is public beta software heading toward a v0.5.0-rc.1 release
candidate, not a finished product. The Windows and macOS (Apple silicon) packages are unsigned, so
expect a SmartScreen/Gatekeeper warning on first run. Task dependencies aren't enforced yet when
starting a run, and project memory has no management UI yet. It's local-only — a loopback listener
on your machine, and your API keys stay in your OS's own credential store.

Repo: https://github.com/10kkyvl/studioforge
Releases: https://github.com/10kkyvl/studioforge/releases

Feedback, bug reports, and pull requests are all welcome — especially from anyone running it
against a real Rojo project.
```

## DevForum announcement (more conservative tone)

```
Title: StudioForge (public beta) — an open-source desktop app for running AI coding agents against your Roblox project

Hi all — sharing an open-source tool I've been building called StudioForge. It's a local desktop
app (a single binary with a browser-based UI) that runs AI coding agents against a Roblox/Rojo
project on your own machine.

The workflow: register a project, describe a change in chat or queue a prompt-driven run, and watch
the agent's output stream live. Before each run, StudioForge takes a git checkpoint, so you get a
per-run diff and can roll back if the result isn't what you wanted. It also has a task board with
dependency tracking, live-sync into an open Roblox Studio session via Rojo, and a Sessions view that
can discover and bind open Studio instances.

You can point it at Claude Code (using your existing local CLI login), OpenRouter, or NVIDIA NIM
(both bring-your-own-API-key), or run it with a built-in --mock demo mode that needs no account at
all.

This is public beta software, currently heading toward a v0.5.0-rc.1 release candidate — expect
rough edges. Windows and macOS (Apple silicon) builds are unsigned for now, so you'll see a
SmartScreen or Gatekeeper warning on first launch. A few things are still partial: task dependencies
aren't yet enforced when a run starts, and project memory (auto-collected from past runs) doesn't
have a management UI yet. Everything runs locally — no external server sees your project, and API
keys are kept in your OS's own credential store.

Repo: https://github.com/10kkyvl/studioforge
Releases: https://github.com/10kkyvl/studioforge/releases

Happy to answer questions about what it does and doesn't do yet — feedback from people running real
Rojo workflows is exactly what this beta needs.
```

## X/Twitter announcement

Fits within 280 characters:

```
StudioForge: an open-source desktop app that runs AI coding agents on your Roblox project. Live
output, per-run diffs, git rollback, task board, Rojo/Studio live-sync. Public beta, unsigned
builds, feedback wanted. https://github.com/10kkyvl/studioforge
```

## Draft GitHub Release — v0.5.0-rc.1

**Title:**

```
v0.5.0-rc.1 — release candidate
```

**Summary:**

```
StudioForge is a free, open-source desktop app for Roblox creators: a single binary with a local
web UI (Go backend, Svelte frontend) that runs AI coding agents against your Roblox/Rojo project.

This release candidate carries forward the public beta: create a project, queue a prompt-driven
run, watch its output stream live, review a per-run diff, and roll back to a git checkpoint if a run
goes wrong. The task board supports dependencies with cycle validation, Rojo live-sync pushes files
into an open Roblox Studio session, and the Sessions view can discover and bind open Studio
instances (manual refresh). Project memory now carries context from completed runs into future ones.

Providers: Claude Code (your local CLI and existing login), OpenRouter, NVIDIA NIM (bring your own
API key for either), or a deterministic --mock demo mode needing no account at all.

Packages: Windows 10/11 (amd64) and macOS (Apple silicon, arm64), both unsigned — expect a
SmartScreen or Gatekeeper warning on first launch. Building from source needs Go and Node.

This is beta software with known rough edges — see docs/KNOWN_LIMITATIONS.md before relying on any
specific behavior. Issues, pull requests, and feedback from real Rojo/Studio setups are very
welcome.
```
