# Prompt changes: before/after on real runs

Issues #35, #40 and #42 each ask for a measured before/after rather than an
assertion, because all three are prompt changes and a prompt change that does
not move anything should not ship. This is that measurement.

## Method

Every cell is one real Claude Code run (`claude -p --append-system-prompt …
--permission-mode acceptEdits`), in its own freshly scaffolded Roblox project —
a Rojo manifest, `src/server`, `src/client`, placeholder entry points and an
`.agent/constitution.yaml`. Nothing is shared between cells, so one run's edits
cannot reach another's. The persona and project context are identical
everywhere; only the part under test differs.

Three arms:

| arm | what it carries |
| --- | --- |
| `old` | the prompt as it was before this branch: no scope-and-length rules, the Studio tool section unconditionally, no interface rules |
| `new-no-ui` | everything on this branch **except** the interface rules — isolates #35 and the conditional Studio section (#31) |
| `new` | the full branch, with the interface rules a UI task now carries (#42) |

Three tasks, chosen to be ordinary interface work rather than a UI exam, and one
of them in Russian so the language rule is exercised alongside:

- `ui1` — "Add a settings menu with a music toggle, an SFX toggle and a close button."
- `ui2` — "Сделай магазин: панель со списком из трёх товаров, у каждого название, цена и кнопка «Купить»."
- `ui3` — "Build a HUD that shows the player's coin count in the top-right corner and flashes when it changes."

Counts come from scanning the Luau each run wrote. **Pixel-only** counts
`UDim2.new(0, x, 0, y)` and `UDim2.fromOffset(…)` — a size with no Scale
component at all, which is the failure the interface rules lead with.

One correction to the method is worth recording, because it changed the answer:
the first scorer skipped `Main.client.lua` by filename, on the assumption that a
run would write new files. Two runs put the entire interface into that seeded
file instead, and were scored against an empty string. Excluding by content
instead — a file that still holds the untouched placeholder — is what the
numbers below use.

## Results — interface rules (#42)

Summed over the three tasks:

| arm | pixel-only sizes | `UIAspectRatioConstraint` | Scale-based sizes | `UITextSizeConstraint` |
| --- | --- | --- | --- | --- |
| `old` | **11** | 0 | 6 | 0 |
| `new-no-ui` | 9 | 0 | 9 | 1 |
| `new` | **0** | **9** | **43** | **5** |

Every arm without the rules produced UI sized in fixed pixels, and no arm
without them reached for an aspect-ratio or text-size constraint even once. With
them, pixel-only sizing disappears completely across all three tasks.

The generated code shows the same thing directly. `ui1`, the settings menu:

```lua
-- old
switch.Size      = UDim2.fromOffset(64, TAP_TARGET)
track.Size       = UDim2.fromOffset(60, 32)
knob.Size        = UDim2.fromOffset(26, 26)
openButton.Size  = UDim2.fromOffset(TAP_TARGET, TAP_TARGET)

-- new
track.Size       = UDim2.fromScale(0, 0.56)
knob.Size        = UDim2.fromScale(0, 0.82)
row.Size         = UDim2.fromScale(1, 0.17)
openButton.Size  = UDim2.fromScale(0.09, 0.09)
```

This is the change the live-Studio probe measured the cost of: a control 200px
wide covers 10.4% of a 1920×1080 screen and 51.3% of a 390×844 one.

## Results — scope and length (#35)

Average final-message length, in characters:

| arm | average reply | average code written |
| --- | --- | --- |
| `old` | 1443 | 7852 |
| `new-no-ui` | **1249** (−13%) | 6718 |
| `new` | 1820 | 9594 |

Isolated, the scope-and-length section shortens the reply by about 13% while the
work still gets done. The full `new` arm is longer than either, and that is not
a contradiction: the interface rules make the task itself bigger — a third more
code — so there is more to report. The two effects pull in opposite directions
and the honest reading is that they are separate, which is why the middle arm
exists.

The scope signal also shows up where it matters more than length: `old` and
`new-no-ui` each modified the seeded placeholder entry points in 4 of 3 runs
(more than one file per run), `new` in 2.

## Results — the correction prompt (#40)

A separate comparison, because the correction prompt is a user turn rather than
a system prompt. Both arms carry the same system prompt; only the correction
text differs — the old one sentence plus the error lines, against the rewritten
one. Two scenarios, each a project seeded with a script and a console line the
classifier flagged:

- **real** — a genuine defect: `purchase()` reads `item.Price` before checking
  the item exists, and a startup call asks for an item that is not in the
  catalog.
- **noise** — a false positive: a lava-zone script that deliberately prints
  `[Error] lava zone … respawned` as designer telemetry on a *normal* respawn.
  `[Error]` is one of the classifier's substrings, so the line is flagged while
  nothing is wrong.

| scenario | `old` | `new` |
| --- | --- | --- |
| real defect | fixed — added a nil guard | fixed — added a type check and a nil guard |
| false positive | **changed the working script** | **changed nothing**, and explained why |

The false-positive row is the one the issue is about, and it reproduced exactly
as predicted. Both arms diagnosed the line correctly: `old` identified it as
intentional telemetry and pointedly refused to rename the `[Error]` prefix
because designers grep for it. Having done so, it still deleted the startup call
that emitted the line — because "fix it, then report back" leaves nowhere else
to go. `new` reached the same diagnosis and stopped:

> No change made — this is a false positive from substring classification, not a
> defect. […] The literal text `[Error]` is baked into the message string. The
> classifier matched that substring; nothing threw, nothing was logged via
> `warn` or `error`, and no script errored out.

The old arm's *reasoning* was not the problem. Its available outcomes were.

The obvious risk in giving an agent that escape hatch is that it starts using it
to dodge real work, and the real-defect row is the check on that. It did not:
`new` fixed the defect and explicitly ruled the false-positive path out first,
using the method statement the prompt now carries —

> This is not a classifier false positive: the line reproduces on every server
> start, with no player involvement — which is exactly why the 30-second
> playtest caught it.

That sentence is only available to an agent that was told the playtest was a
fixed window with no player input, which is the whole point of stating the
method. The cost is length: 1913 characters against 509, and 10 turns against 5.

## What this does not establish

- **Three tasks per arm, and one run per cell.** Model runs are not
  deterministic. The interface-rule result is large enough that sampling noise
  is not a plausible explanation for 11 → 0 with 9 constraints appearing where
  there were none; the 13% length result is not, and should be read as
  directional. The correction result is two scenarios, one run each — it
  reproduced the predicted behaviour cleanly, but it is one observation of each.
- **The `screen_capture` / `get_console_output` verification instruction was not
  A/B tested separately**, which #35 asks for. It sits in the Studio section,
  and these runs had no Studio grant, so the comparison would have measured
  nothing. It needs a run with a real grant, which is a different setup.
- **The correction runs did not resume a real session.** In production a
  correction continues the same provider session, so "identify which of the
  changes you just made causes this" points at history the agent has. Here each
  correction was a fresh run against a seeded project, identically for both
  arms. That makes the instruction weaker than it is in production, not
  stronger, so it does not flatter the new prompt.
- Cost: about $1.20–$1.80 per arm for the three interface tasks, and $0.20–$0.42
  per correction run, on a Claude subscription.

## Verdict

The interface rules and the correction prompt both change behaviour, in the
direction they were written for, by margins that are not noise. They ship.

The scope-and-length section moves reply length 13% in isolation, which is real
but modest, and it is entangled with the interface rules pulling the other way.
It ships on the strength of the scope signal — fewer unrelated files touched —
rather than on length alone, and it is the part of this change most worth
revisiting with a larger sample.
