package prompts_test

import (
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/prompts"
	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
)

// mutatingTools are every Studio tool that changes something. A read-only run is
// denied all of them, so its prompt must not name one.
var mutatingTools = []string{
	"multi_edit", "execute_luau", "generate_mesh", "generate_material",
	"generate_procedural_model", "insert_asset", "search_asset", "wait_job_finished",
	"start_stop_play", "subagent", "skill", "character_navigation",
	"upload_image", "store_image", "http_get", "user_keyboard_input", "user_mouse_input",
}

// namedTools reports which of Studio's tools a piece of prompt text mentions.
// It is checked against mcp.OfficialTools rather than a list kept here, so a
// tool added to the allowlist without a matching rule — or a rule naming a tool
// that no longer exists — shows up as a failure instead of drifting quietly.
func namedTools(text string) []string {
	var found []string
	for _, tool := range mcp.OfficialTools {
		if strings.Contains(text, tool) {
			found = append(found, tool)
		}
	}
	return found
}

func TestStudioSectionSaysNothingWithoutAGrant(t *testing.T) {
	if got := prompts.StudioSection(prompts.StudioAccess{}); got != "" {
		t.Fatalf("a run that never wanted Studio must carry no section, got %q", got)
	}
	if got := prompts.StudioSection(prompts.StudioAccess{Granted: true}); got != "" {
		t.Fatalf("a grant with no tools is not a grant, got %q", got)
	}
	if got := prompts.StudioSection(prompts.StudioAccess{Tools: mcp.AllowedTools("workspace-write")}); got != "" {
		t.Fatalf("tools without a grant must emit nothing, got %q", got)
	}
}

func TestStudioSectionWithoutAGrantNamesNoTool(t *testing.T) {
	notice := "Studio MCP withheld: 3 Studio instances are open and StudioForge cannot pin one"
	got := prompts.StudioSection(prompts.StudioAccess{Notice: notice})
	if !strings.Contains(got, notice) {
		t.Fatalf("the withheld notice must say why, got %q", got)
	}
	if named := namedTools(got); len(named) > 0 {
		t.Fatalf("a run with no Studio must not be told about tools, got %v in %q", named, got)
	}
	if bullets := strings.Count(strings.TrimSpace(got), "\n- "); bullets != 1 {
		t.Fatalf("the withheld notice should be a single line, got %d bullets in %q", bullets, got)
	}
}

func TestStudioSectionForReadOnlyNamesNoMutatingTool(t *testing.T) {
	got := prompts.StudioSection(prompts.StudioAccess{Granted: true, Tools: mcp.AllowedTools("read-only")})
	if got == "" {
		t.Fatal("a granted read-only run still has observation tools to describe")
	}
	for _, tool := range mutatingTools {
		if strings.Contains(got, tool) {
			t.Fatalf("a read-only run must not be told to reach for %q; it would be denied\n%s", tool, got)
		}
	}
	for _, want := range []string{"screen_capture", "get_console_output"} {
		if !strings.Contains(got, want) {
			t.Fatalf("a read-only run keeps its verification tools; missing %q in %q", want, got)
		}
	}
}

func TestStudioSectionForWorkspaceWriteCoversTheToolsItHas(t *testing.T) {
	got := prompts.StudioSection(prompts.StudioAccess{Granted: true, Tools: mcp.AllowedTools("workspace-write")})
	for _, want := range []string{
		"generate_mesh", "generate_material", "generate_procedural_model",
		"search_asset", "insert_asset", "wait_job_finished",
		"subagent", "skill", "screen_capture", "get_console_output", "start_stop_play",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing rule for %q in %q", want, got)
		}
	}
	for _, denied := range []string{"upload_image", "http_get", "user_keyboard_input"} {
		if strings.Contains(got, denied) {
			t.Fatalf("workspace-write does not grant %q, so the prompt must not name it", denied)
		}
	}
}

// The section is generated from the run's allowlist precisely so it cannot name
// a tool the run would be refused. This checks that for every profile at once,
// against the allowlist itself.
func TestStudioSectionNeverNamesAToolTheRunCannotCall(t *testing.T) {
	for _, profile := range []string{"read-only", "workspace-write", "danger-full-access"} {
		allowed := mcp.AllowedTools(profile)
		permitted := map[string]bool{}
		for _, name := range allowed {
			permitted[strings.TrimPrefix(name, mcp.ToolPrefix)] = true
		}
		got := prompts.StudioSection(prompts.StudioAccess{Granted: true, Tools: allowed})
		for _, named := range namedTools(got) {
			if !permitted[named] {
				t.Errorf("profile %q: prompt names %q, which the run may not call", profile, named)
			}
		}
		if len(namedTools(got)) == 0 {
			t.Errorf("profile %q: the section names no tool at all", profile)
		}
	}
}

// An unknown profile grants nothing (mcp.AllowedTools returns nil), and the
// provisioner turns that into a withheld notice. The prompt must follow.
func TestStudioSectionForAnUnknownProfileGrantsNothing(t *testing.T) {
	got := prompts.StudioSection(prompts.StudioAccess{Granted: true, Tools: mcp.AllowedTools("typo-profile")})
	if got != "" {
		t.Fatalf("an unknown profile grants no tools, so there is nothing to say; got %q", got)
	}
}

// The Claude grant carries namespaced names and the in-process bridge strips
// them, so both forms have to mean the same tool here.
func TestStudioSectionAcceptsNamespacedAndBareToolNames(t *testing.T) {
	bare := []string{"screen_capture", "get_console_output", "generate_mesh"}
	namespaced := make([]string, 0, len(bare))
	for _, name := range bare {
		namespaced = append(namespaced, mcp.ToolPrefix+name)
	}
	fromBare := prompts.StudioSection(prompts.StudioAccess{Granted: true, Tools: bare})
	fromNamespaced := prompts.StudioSection(prompts.StudioAccess{Granted: true, Tools: namespaced})
	if fromBare == "" {
		t.Fatal("bare tool names must be understood")
	}
	if fromBare != fromNamespaced {
		t.Fatalf("namespaced and bare names must produce the same section:\n%q\n%q", fromBare, fromNamespaced)
	}
	if strings.Contains(fromNamespaced, mcp.ToolPrefix) {
		t.Fatal("the namespace prefix should not leak into the prose")
	}
}

// A rule that names two tools must drop the one that is missing rather than
// promising it.
func TestStudioSectionDropsToolsMissingFromAPartialGrant(t *testing.T) {
	got := prompts.StudioSection(prompts.StudioAccess{
		Granted: true,
		Tools:   []string{"screen_capture", "generate_mesh", "subagent"},
	})
	if strings.Contains(got, "get_console_output") {
		t.Fatalf("get_console_output was not granted, so it must not be named: %q", got)
	}
	if !strings.Contains(got, "screen_capture") {
		t.Fatalf("screen_capture was granted and must survive: %q", got)
	}
	if strings.Contains(got, "generate_material") || strings.Contains(got, "generate_procedural_model") {
		t.Fatalf("only generate_mesh was granted: %q", got)
	}
	if strings.Contains(got, "skill") {
		t.Fatalf("skill was not granted, so the hand-off rule must name only subagent: %q", got)
	}
	// search_asset/insert_asset is a pair; neither was granted, so the reuse
	// rule has nothing to say.
	if strings.Contains(got, "reuse beats regeneration") {
		t.Fatalf("the reuse rule needs both tools: %q", got)
	}
}

func TestStudioSectionGrantedBeatsANotice(t *testing.T) {
	got := prompts.StudioSection(prompts.StudioAccess{
		Granted: true,
		Tools:   mcp.AllowedTools("read-only"),
		Notice:  "Studio MCP withheld: something",
	})
	if strings.Contains(got, "no Studio connection") {
		t.Fatalf("a granted run must not also be told it has none: %q", got)
	}
}
