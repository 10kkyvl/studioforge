# StudioForge demo recording script (60-120 seconds)

This script is for a short demo video of StudioForge's public beta. It only shows behavior that is
actually implemented. Before recording, re-check anything that looks doubtful against the feature
status table in the [README](../README.md) and [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md) — the
point of this demo is that every second of it is real.

## What this demo shows / does not show

**Shows:**

- Starting the StudioForge daemon from source or a packaged build and reading the printed URL.
- The embedded browser UI opening with an authenticated session (one-use bootstrap token).
- Registering/opening a project and running `studioforge doctor` to confirm detected tooling
  (Git, Claude Code, Rojo, an OpenRouter key if configured, the official Roblox Studio MCP launcher).
- Sending one instruction to a Claude Code agent in the chat and watching the run stream events
  live over SSE.
- The git checkpoint commit StudioForge creates before the run, and the resulting diff.
- Optionally, the place file rebuilt by Rojo and opened in Roblox Studio.
- A closing card stating plainly that this is a public beta.

**Does not show and must not claim:**

- Autonomous or unattended operation. A human sends the instruction and watches the run.
- Task dependencies as an enforced gate — they are persisted and cycle-checked at creation time, but a
  run does not check whether a task's dependencies are finished before starting (see
  [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md)).
- Project memory as a curated or browsable feature — entries are auto-saved from run prompts with no
  management UI or API to list, edit, or delete them.
- Studio Sessions discovery as automatic — real-instance discovery needs a manual **Refresh** click;
  nothing polls the launcher in the background.
- Any capability the official Roblox Studio MCP tooling does not itself provide. StudioForge
  detects and launches that official tooling; it does not reimplement Studio operations and ships
  no Roblox Studio plugin.
- Any user counts, stars, downloads, or contributor numbers. Do not add these to the video or its
  description.

## Setup checklist (before hitting record)

- [ ] Exactly one Roblox Studio instance is open if the Studio/Rojo-open shot is included. StudioForge
      refuses Studio access when more than one instance is open — this is a real, working safeguard,
      not a bug to avoid on camera. Confirm the count with **Studio sessions** or by checking the
      taskbar before recording.
- [ ] A clean, disposable sample Rojo project exists on disk (a small place with a `*.project.json`,
      no unrelated files, nothing under version control that you mind exposing).
- [ ] Claude Code is installed and authenticated (`claude --version`, `claude auth status`) if
      recording the live-Claude variant.
- [ ] Rojo CLI and Studio plugin are installed (`rojo --version`) if the Rojo/Studio shot is included.
- [ ] A terminal window is open, sized, and cleared, with the shell prompt showing no personal
      username or account name (use a generic prompt or crop it out in editing).
- [ ] The browser window is zoomed to a level that keeps UI text legible at video resolution
      (125-150% is usually enough); close unrelated tabs, extensions' badges, and bookmarks bar.
- [ ] Personal absolute paths (e.g. any path under a real user profile) are not visible anywhere on
      screen — use a project path that does not reveal a real name or username.
- [ ] No private repository name, real client name, or real project name is visible in the terminal,
      browser tab titles, editor title bar, or file explorer.
- [ ] Notifications, OS toast popups, and other running apps are closed or muted.
- [ ] Close any Claude Code / Anthropic account or billing page open in another tab.

## Shot list (live-Claude variant, ~90 seconds)

| #   | Time      | Shot                        | On screen                                                                                                                                                                                                                                     | Narration (short, factual)                                                                                                                                                                        |
| --- | --------- | --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | 0:00-0:08 | Clean project on disk       | File explorer or editor showing a small Rojo project: a `*.project.json` and a few source folders.                                                                                                                                            | "This is a small Rojo project on disk — nothing special, just a normal Roblox/Rojo layout."                                                                                                       |
| 2   | 0:08-0:18 | Start StudioForge           | Terminal running the packaged binary (or `./scripts/dev.ps1`/`dev.sh` from source), showing the printed `STUDIOFORGE_URL`.                                                                                                                    | "StudioForge is a single local binary. Starting it prints a URL — the daemon binds to loopback only by default."                                                                                  |
| 3   | 0:18-0:26 | Browser opens authenticated | Browser window opens on the printed URL; UI loads with a valid session (no login form, no visible token in the URL bar after the first load).                                                                                                 | "It opens in the browser with a one-use bootstrap token, then a normal cookie session."                                                                                                           |
| 4   | 0:26-0:38 | Register project + doctor   | Register/open the sample project; open **Doctor** (or run `studioforge doctor` in the terminal) and show detected Git, Claude Code, Rojo, and Studio MCP launcher rows.                                                                       | "Doctor checks what's actually installed — Git, Claude Code, Rojo, and the official Studio MCP launcher — and shows what it found."                                                               |
| 5   | 0:38-0:53 | Send one instruction        | Open the project chat, pick a Claude Code agent, type one clear, concrete instruction (e.g. "add a comment explaining what this module does" or similarly modest), send it.                                                                   | "One instruction, sent to a Claude Code agent registered on this project."                                                                                                                        |
| 6   | 0:53-1:08 | Run streams live            | Show the run view with events arriving live (SSE) — status changes, streamed tool/turn events.                                                                                                                                                | "The run streams events live as it goes, over server-sent events — nothing is polled or faked after the fact."                                                                                    |
| 7   | 1:08-1:20 | Resulting change            | Show the git checkpoint commit StudioForge made before the run, then the diff of what changed in the chat view's diff panel.                                                                                                                  | "Before the run, StudioForge made a git checkpoint so the change is easy to review or revert. Here's the diff."                                                                                   |
| 8   | 1:20-1:28 | Rollback confirmation       | Click **Rollback** on the completed run and show the confirm step (do not confirm unless you intend to actually roll back the disposable sample project on camera). Optionally cut to the place rebuilt by Rojo and opened in Studio instead. | "Rolling back asks for confirmation first — it resets to the checkpoint branch, it doesn't force-delete anything." / (optional) "And here's the same change, built by Rojo and opened in Studio." |
| 9   | 1:28-1:35 | Closing card                | Static card: project name, one line — "Public beta. Feedback and issues welcome." plus the repository URL (https://github.com/10kkyvl/studioforge).                                                                                           | (No narration, or read the card text aloud.)                                                                                                                                                      |

Total: ~95 seconds. Trim shot 8's optional Studio cutaway, or skip actually confirming the rollback, to land anywhere in the 60-120s window.

## Reddit clip (30-45 seconds)

A condensed cut for a short-form post. Same rules as the full shot list apply — nothing shown that
isn't real, same "Do not show" checklist below.

| #   | Time      | Shot                    | On screen                                                                                                          | Narration/caption (short, factual)                               |
| --- | --------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------- |
| 1   | 0:00-0:05 | Start StudioForge       | Terminal (or packaged binary) starting, printed `STUDIOFORGE_URL`.                                                 | "One local binary."                                              |
| 2   | 0:05-0:12 | Wizard/dashboard glance | Quick cut through the first-run wizard's detected-tooling rows, then the dashboard with the registered project(s). | "First-run checks confirm what's installed, then the dashboard." |
| 3   | 0:12-0:25 | Send one instruction    | Open the project chat, pick an agent, type one short concrete instruction, send it.                                | "One instruction to an agent."                                   |
| 4   | 0:25-0:38 | Run streams             | Cut to the run view with live SSE events arriving.                                                                 | "The run streams live — nothing polled or faked after the fact." |
| 5   | 0:38-0:45 | Result/diff             | Show the resulting diff (and rollback button, without necessarily confirming it).                                  | "Checkpointed in git first, so it's easy to review or undo."     |

Total: ~45 seconds. Trim shot 2 or 4 to land at 30s if a tighter cut is needed. The `--mock` fallback
variant below works for this cut too — keep its on-screen mock-data caption for the entire clip if used.

## Fallback variant: `--mock` only (no Claude account, no Studio)

Use this variant when recording without a billable Claude account or without Roblox Studio
installed. Run `studioforge --mock`, which seeds three deterministic, isolated demo workspaces and
exercises the real domain/API and UI without needing Claude, Studio, or Rojo.

Required: an on-screen caption or lower-third for the entire mock segment reading something like
**"Demo data — built-in mock (`--mock`), not a live project or account."** The caption must be
visible any time mock data is on screen, including in thumbnails pulled from this footage.

Suggested shots for the mock variant:

1. Start `studioforge --mock`, show the printed URL (0:00-0:10).
2. Browser opens on the dashboard populated with the three demo projects; captions the data as
   built-in demo data (0:10-0:25).
3. Open a demo project, show tasks/agents/threads already populated by the mock seed (0:25-0:45).
4. Open a run's event history (pre-seeded, not live-streamed) and say plainly that this is replayed
   demo data, not a live run (0:45-1:00).
5. Closing card, same as the live variant, plus a repeated on-screen note that the whole segment was
   `--mock` data (1:00-1:10).

Do not present any `--mock` screen as if it came from a live Claude or Studio session, in the video
or in surrounding text (title, description, thumbnail).

## Do not show

- Real Claude Code authentication tokens, OpenRouter API keys, or session cookies.
- Absolute personal file paths (drive letters plus real usernames, e.g. anything under a real
  `C:\Users\<name>` or `/Users/<name>` profile).
- Private repository names, client names, or project names not meant for a public audience.
- Any account email address, billing page, subscription plan screen, or payment information.
- Any Windows/macOS username visible in a terminal prompt, title bar, or file dialog.
- Star counts, download counts, or any other unverified metric overlaid on the video.
- On the run-diff/rollback shot specifically: the git commit author's real name/email in the
  checkpoint commit metadata, and any absolute personal path shown in the diff's file headers —
  use the disposable sample project's own generic commit identity, not a personal one.

## Recommended README screenshots

`docs/screenshots/` contains `dashboard.png`, `dashboard-900x600.png`, `first-run.png`,
`chat-run.png`, `run-diff.png`, `studio-sessions.png`, and `social-preview.png`, all captured from
the real UI populated with the built-in `--mock` demo data (`social-preview.png` is rendered from a
static HTML card instead — see [docs/screenshots/README.md](screenshots/README.md)). Caption the app
screenshots as demo data in the README, and do not present them as a live project.

| File                                      | Shows                                                                                                                             | Must be cropped out                                                       |
| ----------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `dashboard.png` / `dashboard-900x600.png` | Projects view listing the three `--mock` demo projects (Skyline Obby, Harbor Tycoon, Neon Arena) with status, budget, and agents. | Personal paths, real usernames, private project names.                    |
| `first-run.png`                           | The first-run setup wizard's detected-tooling checks.                                                                             | Personal paths, real usernames.                                           |
| `chat-run.png`                            | A project chat thread with agent/team selection and a run in progress.                                                            | Real prompts referencing private work, personal paths, tokens.            |
| `run-diff.png`                            | A completed run's diff panel with the rollback confirmation UI.                                                                   | Commit author email if personal, absolute personal paths in diff headers. |
| `studio-sessions.png`                     | The Studio Sessions view with its (mock) session chips.                                                                           | Studio account name/avatar, private place names, personal paths.          |
| `social-preview.png`                      | The static social-preview card (not app UI) for GitHub's social preview slot.                                                     | N/A — no captured UI, nothing to crop.                                    |

No fabricated or mocked-up UI — anything drawn, prototyped, or edited to look like the product — may
be presented as if it were a real captured screenshot, in the README or anywhere else.
