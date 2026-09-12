# DataModel snapshots: design decision for #18

Date: 2026-09-13. Status: reviewed research decision — defer production implementation.

Recommendation: defer automatic snapshots. The small-place capture measured
below is cheap, but MCP transport was unavailable and representative-scale
end-to-end cost is still unknown. The existing dispatch journal remains useful,
but is not evidence of a complete before/after state and cannot implement undo.

## Scope and capture

Use explicit operator-selected roots, defaulting to no roots. Do not infer the
scope from an agent's prose. Rojo roots may be offered as suggestions but require
an explicit selection: source-controlled scripts and Studio-only world state
are different surfaces. Capture in Edit mode before the first mutating action
and after the run, using the same retained Studio grant. Never open a second
launcher while a run owns Studio. Skip with a visible reason if the grant is
unavailable, the place changes, or either capture exceeds the limit.

First implementation limits: 1,000 instances, 1 MiB serialized response per
capture, and 2 seconds per capture. These are proposed safety bounds, not
measured performance claims. A truncated capture must be marked incomplete;
missing records after truncation must never be described as deleted instances.

## Identity, properties, and differences

Store class, name, parent relation, sibling occurrence, and a versioned property
schema. Start with BasePart transforms, dimensions, collision flags and color;
add class-specific adapters for Models, UI, and lighting after independent
validation. Read property adapters under pcall. Unsupported or unreadable
properties are unknown, not empty. Never capture Source, credentials, arbitrary
attribute contents, or the entire properties surface by default.

A read-only snapshot has no guaranteed stable identity across rename/move when
siblings share names. Match unambiguous paths first; an unmatched object is
added/removed. Report a move only with a stable identity from an explicitly
supported surface. Do not silently write tracking attributes into the user's
place just to improve matching. Include capture timestamps and detect a changed
place ID. Concurrent manual/Team Create changes remain unattributable to the
agent; the UI must say the diff is observed state, not authorship.

## Storage and retention

Store compressed snapshot blobs outside the event log, keyed by run and schema
version; keep hashes and summary metadata in SQLite. Default to retaining the
last 20 snapshot pairs per project and a 50 MiB project cap. Delete oldest
un-pinned pairs first; show when a snapshot expired. Export or pin is explicit.
Failure writing the second capture must leave a visible incomplete pair, not a
fabricated empty after-state. Enforce size limits before storage and account for
uncompressed size to prevent decompression resource exhaustion.

## Undo

This design does not enable undo. The selected property set is incomplete,
identities can be ambiguous, external references are not restored, and concurrent
edits would be overwritten. Undo requires its own transactional Studio design
and conflict detection; a Git rollback must continue to state its scope.

## Cost probe and current evidence

Run `STUDIOFORGE_REAL_STUDIO=1 go test ./internal/roblox/mcp -run '^TestRealSnapshotResearch$' -v -count=1`
with exactly one disposable test place open and Studio MCP enabled. The probe
performs five read-only captures of up to 1,000 Workspace instances, reporting
instance count, encoded bytes, Luau capture time, truncation and MCP round-trip
time. It does not change Studio mode or write any instances or attributes.
Record machine, place scale, median and maximum round trip, and estimate a pair
as two measured captures. Repeat on a small and representative larger place
before recommending build.

The connection preflight on this Apple Silicon macOS machine on 2026-09-13
advertised zero tools and timed out after 20 seconds while listing Studio
instances (`TestRealStudioMCP`). Therefore no successful real-place capture cost
is available in this revision. Connection timeout is not a snapshot benchmark.
A second attempt through ProvisionLive returned no connected instance.


### Actual real-place measurement

The open local `StudioForge-Smoke.rbxl` was measured on 2026-09-13 via Studio's
Command Bar in Edit mode. This is a read-only fallback while MCP is unavailable;
it measures traversal plus serialization, **not** MCP transport or model cost.
Five captures of Workspace descendants each contained 3 instances (Camera,
Terrain, Baseplate), with 549 serialized bytes. Source and attributes were not
read and the place was not modified.

| Sample | Capture + JSON time |
| --- | --- |
| 1 | 0.063125 ms |
| 2 | 0.017500 ms |
| 3 | 0.010250 ms |
| 4 | 0.008375 ms |
| 5 | 0.007292 ms |

Median: 0.010250 ms; maximum: 0.063125 ms. A before/after pair for this **tiny
place** is an estimated 1,098 uncompressed bytes and 0.020500 ms local capture
time at the measured median, excluding transport and persistence. Twenty pairs
are about 22 KB of raw JSON before metadata. Do not extrapolate linearly to a
large place; GetDescendants allocates its result up front. The automated probe
uses bounded breadth-first traversal instead and also includes the root.

Reviewed decision: defer. The measured local lower bound rules out serialization
as a problem on this tiny fixture but cannot justify two extra Studio MCP calls
per production run. The absence of a working MCP connection is itself a reason
to avoid integrating automatic captures now. A future implementation issue must
first record the automated probe on a representative larger place, including
median/maximum round-trip cost and incomplete-capture handling. No implementation
commitment or Studio undo capability is implied by closing this research task.
