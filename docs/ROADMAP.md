# Roadmap

This roadmap has no dates. Order within and across sections is not a commitment, and it will change
based on real feedback from people using the alpha, not on a predetermined schedule.

## Current alpha stabilization

Work needed to make what already exists in the repository trustworthy for an alpha user, rather than
adding new surface area:

- Wire up or deliberately remove each implemented-but-unreachable package instead of leaving it as
  dead code with no caller: project memory (`internal/memory`), the task dependency DAG
  (`internal/tasks/dag.go`), git status/diff/rollback (`internal/gitops`), asset quarantine
  (`internal/roblox/assets`), and Rojo live-sync sessions (`internal/rojo` session manager, as
  opposed to the build-and-open path that is already wired).
- Replace the demo-only rows in the Studio Sessions view with real Roblox Studio instance discovery;
  today those rows are seeded only by the mock demo.
- Give Decisions a real producer. The `resolveDecision` endpoint and `DecisionsView` exist, but no
  live run currently creates a decision — only the mock demo seed does.
- Add tests for `internal/diagnostics` (`studioforge doctor` and the diagnostic bundle), which
  currently has none.
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

- Persistent project memory as an actual workflow feature that runs write to and read from, built on
  top of the existing (currently unused) `internal/memory` store.
- Visual feedback and screenshot-driven iteration.
- Automated playtest validation.
- Multi-agent orchestration beyond the current orchestrator-to-`--agents` delegation that Claude Code
  already provides natively.
- Autonomous, long-running agent loops.
