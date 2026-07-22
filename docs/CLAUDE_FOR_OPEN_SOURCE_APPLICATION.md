# Claude for Open Source — application answers

This file holds draft answers for StudioForge's application to Anthropic's Claude for Open Source
program. It is a working draft, not a submission record: re-read every field against the current
state of the repository immediately before pasting it into the application form, since alpha-stage
details (what is wired up, what is unsigned, what test coverage exists) can change between now and
submission time.

---

## Field 1: Tell us about the project's reach and impact

```
StudioForge has just become public as an alpha release. It has no accumulated reach metrics —
no meaningful star count, download count, or contributor history yet — and this answer does not
lean on any of that, because there isn't any to report honestly.

The gap it addresses is structural rather than a matter of adoption. Roblox development happens
inside a proprietary editor (Roblox Studio), against a Luau codebase, with place files and external
synchronization tooling such as Rojo. General-purpose AI coding tools are not built around any of
that: they model text files in a repository well, but not a running Studio session, a place file's
structure, or a Rojo sync workflow. The Roblox creator ecosystem is large and skews heavily toward
independent developers and small teams working without dedicated tooling staff — that is a
qualitative description, not a cited figure, and this answer does not attach a number to it. AI
tooling aimed at this ecosystem is also fragmented: separate, disconnected pieces exist for chat,
for code generation, and for Studio automation, with little that ties them into one repeatable
project workflow.

StudioForge connects several pieces that already exist into a single workflow: Claude Code as a
coding agent, Roblox Studio's own official MCP tooling for reaching into a live Studio session,
source control (an automatic git checkpoint before every change, so a run is always easy to review
or revert), lightweight project-level context (version-controlled constitution and requirements
files included verbatim in an agent's system prompt), and basic validation (a doctor command that
verifies what tooling is actually installed and configured). It is a complement to Roblox Studio's
official MCP launcher, not a substitute: StudioForge detects and starts that official launcher and
speaks MCP to it, and contains no reimplementation of Studio operations and no Roblox Studio plugin
of its own.

This is a newly released open-source alpha focused on improving AI-assisted development workflows
for Roblox Studio. No claim is made that any user currently depends on it — it is too new for that
to be true. The plausible benefit is for independent developers and small teams who want an AI
coding agent to work against a real Roblox/Rojo project with some guardrails (one writer per
project, a required git checkpoint, an explicit tool allowlist) rather than as an unstructured
script. Although the repository does not yet meet the program's standard reach thresholds, it
addresses an underserved development ecosystem and explores how Claude Code can support complete
project-level workflows rather than isolated code generation. Releasing it early, in the open, is
the fastest honest way to get feedback from real Rojo users and real Roblox Studio setups that a
closed pre-release process could not reach.
```

---

## Field 2: How will you use the subscription for your project?

```
A Claude Max subscription would be used directly on StudioForge's own development, in ways that
consume real, sustained usage rather than occasional code generation:

- Ongoing development of the open-source Go and SvelteKit codebase.
- Testing long Claude Code sessions end to end, since StudioForge's own Claude Code provider execs
  a real `claude` binary in non-interactive print mode and streams its output — verifying this
  requires running actual sessions, not just unit tests against a fake CLI.
- Implementing and verifying the Roblox Studio MCP integration: launcher discovery, per-run MCP
  config generation, and the tool allowlist enforced per agent permission profile.
- Work on provider adapters: Claude Code's CLI flags and event formats as it evolves, and the
  OpenRouter in-process agent loop's tool-calling, model catalog, and cost-tracking behavior.
- Analyzing multi-file changes and testing context retention across long-running sessions, which is
  central to a project-level workflow tool and cannot be validated with short prompts.
- Building and extending automated validation (the doctor diagnostics, integrity checks, redaction).
- Testing against real example Rojo/Roblox projects rather than synthetic fixtures only.
- Diagnosing and fixing synchronization failures between StudioForge, Rojo, and Roblox Studio.
- Preparing and maintaining documentation as functionality changes.
- Reviewing community issues and pull requests once the project is public.
- Shipping regular alpha releases as fixes and features land.

The specific reason this needs a higher-tier plan rather than a lighter one is architectural: every
StudioForge run execs a real `claude` subprocess and inherits the operator's own Claude Code
configuration (CLAUDE.md, hooks, plugins, skills), so a single realistic test run already consumes
meaningful usage, not a trivial prompt-and-response exchange. On top of that, StudioForge's
orchestrator path passes other enabled agents to Claude Code's native `--agents` flag as subagents,
so testing that delegation path multiplies usage further within a single run. Verifying this
behavior honestly — rather than assuming it works from a short manual check — means running it
repeatedly under realistic conditions, which is what sustained project-level workflow testing
requires. No specific delivery dates or outcomes are promised here; this describes how the
subscription would be used, not a committed roadmap.
```

---

## Field 3: Other info

```
StudioForge is not a copy of the official Roblox Studio MCP tooling. It detects Roblox Studio's own
official MCP launcher (the Windows mcp.bat / macOS StudioMCP process) and starts it as a
subprocess, then speaks real MCP JSON-RPC to it. Around that, StudioForge adds project-level
orchestration (a fair scheduler, writer leases, budgets, pause/resume/cancel/restart), lightweight
context (two version-controlled files included verbatim in an agent's system prompt), a permission-
scoped tool allowlist, and a required git checkpoint before every non-plan run. None of that
reimplements what Studio's MCP server itself does.

The core project will remain open source under the MIT license.

The architecture is deliberately built to be extensible: provider adapters (Claude Code as a local
CLI, OpenRouter as an HTTP API with its own in-process agent loop, and a mock provider) are kept
separate from the domain packages that model projects, tasks, agents, and runs, and Studio tool
access is scoped by an explicit permission-profile allowlist rather than
hard-coded per feature. This separation is meant to make it realistic to add another provider
adapter or another Studio capability without reworking the domain layer.

It is being released early, as an alpha, specifically to develop it against real-world feedback
rather than in isolation. The current state is labeled honestly as alpha throughout the
documentation, including the fact that some implemented, unit-tested packages — project memory,
a task dependency graph, git status/diff/rollback endpoints, asset quarantine, and Rojo live-sync
session control — are not yet wired into the running UI or API, and are documented as not
user-reachable rather than advertised as working features. Real end-to-end paths against a live
Claude account or a live Roblox Studio session are covered only by opt-in smoke tests, not by
default CI, and that gap is stated in the repository's own known-limitations documentation as well.

Repository URL: <REPOSITORY_URL>
Demo URL: <DEMO_URL>
Documentation URL: <DOCUMENTATION_URL>
Release URL: <RELEASE_URL>
```
