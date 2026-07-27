package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Claude runs are a subprocess, and setting its working directory is a starting
// point rather than a boundary. What StudioForge can do about that depends
// entirely on the permission mode the profile maps to, and the difference is
// worth stating plainly rather than implying parity across profiles:
//
//	profile              permission mode     deny rules   PreToolUse hook
//	read-only            default             apply        runs
//	workspace-write      acceptEdits         apply        runs
//	danger-full-access   bypassPermissions   ignored      does not run
//
// So two of the three profiles can be narrowed and the third cannot. Nothing
// here is an OS-level boundary either way — it is Claude Code checking itself,
// and Claude Code's own sandbox is unavailable on native Windows, which is where
// StudioForge mostly runs. A run that gets past this is not contained; it is
// contained only as far as the CLI honours its own settings.
//
// The hook is what actually holds. It resolves the path a file tool was given
// against the project root, using the same containment agenttools already
// applies to StudioForge's own tools, and refuses anything outside. The deny
// rules below are a backstop for a CLI whose settings file is read but whose
// hooks are not: they name credential stores specifically rather than sweeping
// globs, because a project living under the operator's home directory would
// otherwise be denied along with the secrets.

// confinedTools are the tools whose arguments name a path this can actually
// check. Bash is deliberately absent: a shell command cannot be reduced to a
// path, and a heuristic that tried would refuse ordinary work — git reading its
// global config, a build tool writing to a cache in the home directory — while
// still missing anything determined. That gap is documented rather than papered
// over.
const confinedTools = "Read|Edit|Write|MultiEdit|NotebookEdit|Glob|Grep"

// credentialStores are denied outright, as a backstop for the case where the
// settings file is honoured but the hook is not.
var credentialStores = []string{
	"~/.ssh/**", "~/.aws/**", "~/.gnupg/**", "~/.config/gh/**", "~/.docker/config.json",
}

// confinementSettings is the Claude Code settings document StudioForge
// generates per run.
type confinementSettings struct {
	Permissions struct {
		Deny []string `json:"deny"`
	} `json:"permissions"`
	Hooks struct {
		PreToolUse []hookMatcher `json:"PreToolUse"`
	} `json:"hooks"`
}

type hookMatcher struct {
	Matcher string        `json:"matcher"`
	Hooks   []hookCommand `json:"hooks"`
}

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// confinable reports whether a profile's permission mode honours any of this.
// bypassPermissions ignores deny rules and skips PreToolUse hooks entirely, so
// writing a settings file for danger-full-access would claim a containment that
// does not exist.
func confinable(profile, mode string) bool {
	if mode == "plan" {
		return true
	}
	return profile == "read-only" || profile == "workspace-write"
}

// writeConfinement generates the per-run settings file and returns its path
// together with a cleanup. ok is false whenever confinement does not apply or
// cannot be written, and a false there degrades the run rather than failing it —
// the same posture the capability gate takes for a flag the CLI does not have.
func writeConfinement(dir, profile, mode, runID string, self func() (string, error)) (string, func(), bool) {
	if dir == "" || !confinable(profile, mode) {
		return "", nil, false
	}
	exe, err := self()
	if err != nil {
		return "", nil, false
	}
	var settings confinementSettings
	settings.Permissions.Deny = denyRules()
	settings.Hooks.PreToolUse = []hookMatcher{{
		Matcher: confinedTools,
		Hooks:   []hookCommand{{Type: "command", Command: quoteCommand(exe) + " claude-guard --root " + quoteCommand(dir)}},
	}}
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", nil, false
	}
	name := "studioforge-confine-" + sanitizeRunID(runID) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".json"
	path := filepath.Join(os.TempDir(), name)
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		return "", nil, false
	}
	return path, func() { _ = os.Remove(path) }, true
}

func denyRules() []string {
	rules := make([]string, 0, len(credentialStores)*3)
	for _, store := range credentialStores {
		rules = append(rules, "Read("+store+")", "Edit("+store+")", "Write("+store+")")
	}
	return rules
}

// quoteCommand wraps a path for the shell the hook command is run through.
// Windows paths carry spaces often enough — "Program Files", a user name with a
// space — that an unquoted one is a broken hook rather than a rare edge.
func quoteCommand(path string) string {
	return `"` + path + `"`
}

// sanitizeRunID keeps a run ID usable as part of a file name without trusting it
// to be one.
func sanitizeRunID(runID string) string {
	out := make([]rune, 0, len(runID))
	for _, r := range runID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "run"
	}
	return string(out)
}
