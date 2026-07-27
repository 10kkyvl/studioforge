# Agent-authored tests in Studio

Issue #41 asks whether StudioForge should let an agent write and run real tests
inside Roblox Studio instead of entering Play mode, waiting thirty seconds and
reading the console. It asks for a reviewed design and an explicit
recommendation, not for code.

## The problem, stated precisely

The validation loop enters Play mode, polls `get_console_output` for a bounded
window, takes one screenshot, and classifies what it saw. **Nobody plays the
game.** No input is driven, no event is fired, no assertion is made.

That bounds what the loop can ever catch to failures that surface on their own
during startup: script errors, infinite yields, nil indexing at load. It cannot
catch the failures a Roblox creator actually cares about — the platform that
does not move, the button that does nothing, the leaderboard that never updates,
the jump that is now twice as high, the shop that hands out an item for free.

Two recent changes narrow the overclaim without changing what can be observed.
#36 renamed the pass-like outcome to `no_errors_detected` and made it conditional
on evidence that the place actually entered Play mode. #23 replaced phrase
matching with parsing over what Studio really prints. Both make the existing
signal honest and more reliable. Neither makes it a test.

## Feasibility, established against a live Studio

The issue is explicit that the execution path must be settled first, because
everything else depends on it. It is settled. `TestRealStudioErrorShape`
(`internal/roblox/mcp/error_shape_smoke_test.go`), run against an open Studio
that was in Play mode at the time, reported:

```
TestService.reachable=true
TestService.Error=true      TestService.Message=true
TestService.Check=true      TestService.Done=true
TestService.Warn=true       TestService.Fail=true
TestService.ExecuteWithStudioRun=true
HttpService.JSONEncode=true
json.sample={"cases":["a","b"],"passed":2,"failed":1}
RunService.IsRunning=true   RunService.IsStudio=true
```

Three facts follow, and together they answer the load-bearing question **yes**:

1. **`execute_luau` reaches the running game.** Its `datamodel_type` accepts
   `Edit`, `Server` and `Client` — and only those. `Play`, `PlayServer`,
   `PlayClient`, `Running`, `Game` and lowercase spellings are all rejected with
   `Invalid datamodel_type`. `Edit` is refused outright while Studio is playing
   (`Edit datamodel is not available in Play mode`), while `Server` and `Client`
   both answered from inside the running game, with `RunService:IsRunning()`
   true. This was not documented anywhere in the repo and is the single most
   useful thing the probe established.
2. **`TestService` is fully reachable**, including `ExecuteWithStudioRun`.
3. **A structured report can come back.** `HttpService:JSONEncode` works, and the
   encoded string returns through `mcp.TextResult` intact — which matters,
   because `TextResult` requires exactly one text block and rejects `isError`.

So an agent-authored Luau test can be executed against a live game and return a
machine-readable result today, with the tool surface StudioForge already has.
No platform request is needed. That is a stronger answer than the issue expected.

## The questions the issue requires answering

### Execution path

`execute_luau` with `datamodel_type: "Server"` for anything authoritative
(physics, data stores, remote handlers, economy) and `"Client"` for anything the
player sees. The test body ends by returning `HttpService:JSONEncode(report)`;
StudioForge reads it with `TextResult` and decodes.

`TestService` is reachable but is **not** the right carrier. Its methods report
into Studio's own output window, which puts the result back into the same
unstructured console this whole issue exists to stop depending on. Use it only
if a test wants its failures visible to a human watching Studio; the JSON return
value is the contract.

The report schema should be small and closed:

```jsonc
{
  "suite": "shop",
  "cases": [
    { "name": "buying an item deducts its price", "passed": true,  "ms": 12 },
    { "name": "buying without funds is refused",  "passed": false, "ms": 8,
      "message": "expected balance 100, got 0", "script": "ServerScriptService.Shop", "line": 41 }
  ]
}
```

`script` and `line` deliberately mirror `mcp.ConsoleEntry`, so a failed case and
a console error can be shown to the operator by the same run-view component.

### Who writes the tests

**Agent-authored per feature, and nothing more, for a first version.** An
accumulating project-level suite is more valuable and much harder: it has to
survive the agent editing it, which means a policy for what an agent may change
in a test it did not write, plus a way to tell "this test is now wrong" from
"this test is now inconvenient". That is a second issue, not this one.

A test lives beside the feature under `src/**/__tests__/` and is committed with
it, so the existing per-run Git checkpoint and diff already cover it and no new
storage is needed for the test text itself.

### Trust

An agent that writes its own tests can write tests that pass. `return {passed =
true}` satisfies any schema.

Two defences, both cheap:

- **Independence.** The verifier is a subagent that did not write the
  implementation. StudioForge already has the mechanism — `--agents` on Claude
  (`internal/providers/claudecode/claude.go`), with subagents already given
  `prompts.NoQuestions` (`internal/api/api.go`). The verifier is handed the
  feature description and the diff, not the implementer's reasoning.
- **Falsifiability.** A test that never fails proves nothing, so the runner
  executes each case twice: once against the change, once against the
  checkpoint commit taken before the run (`internal/gitcheckpoint`). A case that
  passes in both states did not test the change. This is the check that makes
  the whole idea worth something, and it costs one extra run of the same suite.

### Where results live

A new table rather than a column. The current outcome is one string on `runs`
(`007_run_validation.sql`); named cases are a different shape.

```sql
CREATE TABLE run_test_cases (
  id         TEXT PRIMARY KEY,
  run_id     TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  suite      TEXT NOT NULL,
  name       TEXT NOT NULL,
  passed     INTEGER NOT NULL,
  message    TEXT,
  script     TEXT,
  line       INTEGER,
  duration_ms INTEGER,
  created_at TEXT NOT NULL
);
```

`runs.validation` keeps its meaning and gains one value, so a run that ran a
suite is distinguishable from one that only watched the console.

### Relationship to the existing loop

The playtest **survives as the cheap first filter**, unchanged. It costs one
Play-mode entry and catches startup errors before a suite is worth running. If
it comes back `failed`, no suite runs — there is nothing to assert against a
game that did not load. This replaces the playtest as the *primary* signal, not
as a mechanism.

### Cost

Opt-in per agent, exactly like `validateAfterRun` (`internal/models/models.go`),
and off by default. Writing a test is model time on every run; running it twice
for the falsifiability check doubles the Studio time. Default-on for any agent
type would make every trivial change pay for a suite.

## Recommendation

**Build it, in two issues, and not as one.**

1. **The runner.** `execute_luau` on the `Server`/`Client` datamodel, the JSON
   report schema, the `run_test_cases` table, and the run view. No agent
   authoring yet — the first tests are written by hand, which is how the runner
   gets proven before anything depends on it. This is the piece the probe has
   already de-risked.
2. **Agent authoring plus the trust model.** Verifier subagent, the
   before/after falsifiability check, and the prompt work to make an agent write
   a test worth having.

Do **not** build the accumulating project-level suite yet. It is where the real
value is, and it is the part with an unsolved problem in it — an agent editing
tests it did not write — which deserves its own design rather than being carried
along by this one.

## What this does not settle

- Whether an agent can drive input. `user_keyboard_input`, `user_mouse_input`
  and `character_navigation` exist, but sit in `reachingTools`
  (`internal/roblox/mcp/config.go`), i.e. `danger-full-access` only, because
  they deliver synthetic input to the operator's desktop. A test needing real
  input is therefore unavailable to exactly the profile most runs use. Firing
  the event directly from the test body avoids this for most cases and should
  be the documented approach.
- How long a test may run before the harness gives up, and what a hung
  `execute_luau` does to the writer lease.
- Whether `Server` and `Client` can be exercised in one pass or need two calls.
