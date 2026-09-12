# Roadmap

This roadmap has no dates. Order within and across sections is not a commitment, and it will change
based on real feedback from people using the beta, not on a predetermined schedule.

## Current beta stabilization

Work needed to make what already exists in the repository trustworthy for a beta user, rather than
adding new surface area:

- Project-memory management UI/API is shipped: operators can list, edit, pin, delete, and clear entries;
  the latest run shows which entries were injected into its prompt.

## Near-term

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

- A richer project memory than the current curated store: entries still originate from run prompts and
  selection is relevance based, without summarization of what actually happened.
- Richer visual iteration than the current pasted-image and `screen_capture` handoff, such as
  automatic comparison of several playtest views.
- Multi-agent orchestration beyond the current orchestrator-to-`--agents` delegation that Claude Code
  already provides natively.
- Autonomous, long-running agent loops beyond the bounded correction-run chain the validation loop
  now schedules.
- Background polling for the Studio Sessions view is available in Settings. It defaults
  to a conservative 60-second interval (`studio_sessions_poll_interval_seconds=60`), can be disabled
  with 0, and is suspended while any Claude,
  OpenRouter, or NVIDIA run or Studio validation holds a grant. A grant cancels and drains an
  in-flight listing before it opens its own launcher connection, and a short quiet gap follows the
  last release so a correction run cannot race the next poll.
