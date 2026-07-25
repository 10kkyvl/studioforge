package prompts

import "strings"

// StudioAccess is a run's resolved Studio MCP access, as the prompt layer needs
// to see it. It is deliberately stated in terms of what the run may call rather
// than how access was decided, so the scheduler and the in-process agent loop
// can both build it from their own grant type without either of them, or this
// package, learning the other's shape.
type StudioAccess struct {
	// Granted reports whether the run actually has a Studio connection.
	Granted bool
	// Tools are the tool names the run may call, exactly as its grant computed
	// them. Namespaced forms ("mcp__Roblox_Studio__script_read") are accepted
	// alongside bare ones: only the part after the final "__" is matched, so a
	// change to the MCP server name cannot silently empty this list.
	Tools []string
	// Notice explains why access was withheld, when something was expected and
	// refused. Empty means nothing was expected — a machine with no Studio open
	// is an ordinary setup, not a problem to report.
	Notice string
}

// bareToolName strips an MCP namespace prefix, so "mcp__Roblox_Studio__script_read"
// and "script_read" are the same tool to this package.
func bareToolName(name string) string {
	if idx := strings.LastIndex(name, "__"); idx >= 0 {
		return name[idx+len("__"):]
	}
	return name
}

// permittedTools indexes a grant's tool list for lookup by bare name.
func permittedTools(names []string) map[string]bool {
	permitted := make(map[string]bool, len(names))
	for _, name := range names {
		if bare := strings.TrimSpace(bareToolName(name)); bare != "" {
			permitted[bare] = true
		}
	}
	return permitted
}

// present returns the candidates the run may actually call, in the order given.
func present(permitted map[string]bool, candidates ...string) []string {
	var found []string
	for _, name := range candidates {
		if permitted[name] {
			found = append(found, name)
		}
	}
	return found
}

// orList renders tool names the way the prose around them reads: "a", "a or b",
// "a, b or c".
func orList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " or " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// StudioSection is the Studio half of a run's system prompt, composed from the
// run's actual grant rather than emitted unconditionally.
//
// Every tool this section names is checked against the grant's own allowlist
// first, so the prompt cannot describe a capability the run does not have: a
// read-only run is never told to reach for a mutating tool it would be denied,
// and a run with no Studio at all is not handed a page about tools that are not
// there. That also removes a drift risk — the tool names live here and in the
// allowlist, and a bullet naming a tool nobody grants simply stops being
// emitted rather than quietly going stale.
//
// The result is appended after everything ForRun composed: it is the most
// run-specific part of the prompt, so it belongs last (see ForRun).
func StudioSection(access StudioAccess) string {
	permitted := permittedTools(access.Tools)
	if !access.Granted || len(permitted) == 0 {
		notice := strings.TrimSpace(access.Notice)
		if notice == "" {
			// Nothing was expected and nothing was refused. Saying "you have no
			// Studio" on a run that never wanted one is the same unconditional
			// noise this section exists to remove.
			return ""
		}
		return "## Roblox Studio\n\n- This run has no Studio connection — " + notice +
			". Describe the Studio-side work that is needed, and why, instead of attempting it: none of the Studio tools are available to you here, and there is nothing to retry."
	}

	var bullets []string
	generators := present(permitted, "generate_mesh", "generate_material", "generate_procedural_model")
	if len(generators) > 0 {
		bullets = append(bullets, "Reach for "+orList(generators)+" before hand-rolling Luau to produce new 3D content — they exist so you don't have to fake geometry, textures or procedural shapes with scripts.")
	}
	if permitted["search_asset"] && permitted["insert_asset"] {
		bullets = append(bullets, "Before generating an asset from scratch, run search_asset and, if something usable turns up, insert_asset instead — reuse beats regeneration.")
	}
	if asyncProducers := present(permitted, append(append([]string{}, generators...), "insert_asset")...); permitted["wait_job_finished"] && len(asyncProducers) > 0 {
		bullets = append(bullets, "Generation and asset jobs run asynchronously: after kicking one off, call wait_job_finished before you inspect, place or build on its result.")
	}
	if handoffs := present(permitted, "subagent", "skill"); len(handoffs) > 0 {
		bullets = append(bullets, "When a piece of Studio-side work is well-scoped and would otherwise clutter your own context, hand it off with "+orList(handoffs)+" rather than doing it inline.")
	}
	if verifiers := present(permitted, "screen_capture", "get_console_output"); len(verifiers) > 0 {
		bullets = append(bullets, "Don't assume a script \"just worked\" — confirm with "+orList(verifiers)+" before reporting a visual or gameplay result as done. You cannot see the running game; these are the only things that can tell you what happened.")
	}
	if permitted["start_stop_play"] {
		bullets = append(bullets, "start_stop_play changes play state rather than confirming it: use it to enter or leave Play mode, not as evidence that something works.")
	}
	if len(bullets) == 0 {
		return ""
	}
	return "## Using the Studio MCP tools\n\nThis run is connected to Roblox Studio. You may call the Studio tools your toolset lists and no others — a call to anything outside them is refused by design, not by accident.\n\n- " +
		strings.Join(bullets, "\n- ")
}
