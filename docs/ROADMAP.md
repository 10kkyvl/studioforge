# Roadmap

This roadmap has no dates. Order within and across sections is not a commitment, and it will change
based on real feedback from people using the alpha, not on a predetermined schedule.

## Current alpha stabilization

Work needed to make what already exists in the repository trustworthy for an alpha user, rather than
adding new surface area:

- Gate run execution on task-dependency readiness. Dependencies are now persisted and validated as a
  DAG at creation time (`internal/tasks/dag.go`), but nothing yet stops a run from starting against a
  task whose dependencies aren't done.
- Wire up or deliberately remove the packages still implemented but unreachable: git rollback and tag
  (`internal/gitops.SafeRollback`/`Tag` — `Status` and `DiffHead` are wired). Rojo live-sync sessions
  (`internal/rojo` session manager) are wired (`POST`/`DELETE /api/v1/projects/{id}/sync`); recent log
  lines from a live session are not yet surfaced to the API or UI.
- Add automatic pruning for persisted run events. Retention is schema-ready today but depends on
  manual database maintenance.

## Near-term

- Codex access to Roblox Studio MCP, if and when a configuration mechanism appears for it. The Codex
  CLI has no `--mcp-config`-equivalent today, so this is blocked on upstream Codex, not on
  StudioForge's own code.
- Project context beyond the two static files read verbatim today
  (`.agent/constitution.yaml`, `.agent/requirements.md`). Any richer context mechanism needs its own
  design before it is added.
- Signed macOS and Windows packages, once a maintainer holds a valid Apple Developer ID and a Windows
  Authenticode certificate. Both packages currently ship unsigned.
- Broader OS/architecture coverage beyond the two targets built and packaged today
  (Windows amd64, macOS arm64).

## Later exploration

Everything in this section is **RESEARCH**: an idea under consideration, with no committed design
and no implementation. Listing something here is not a promise it ships, and it may not resemble this
description if it ever does.

- A richer project memory than the minimal version now live: today a run writes its own prompt text
  and the next run's system prompt gets a handful of relevant past prompts back, with no summarization
  of what actually happened and no UI to browse or curate what's stored.
- Visual feedback and screenshot-driven iteration beyond the validation loop's single screenshot per
  playtest pass.
- Multi-agent orchestration beyond the current orchestrator-to-`--agents` delegation that Claude Code
  already provides natively.
- Autonomous, long-running agent loops beyond the bounded correction-run chain the validation loop
  now schedules.
- Background polling for the Studio Sessions view, instead of the operator's own **Refresh** click.
  Deliberately not done yet: every probe spawns a launcher process that competes with a running agent
  for Studio's single WS host slot, so an unattended poll interval trades that risk for convenience
  this alpha does not yet need.
