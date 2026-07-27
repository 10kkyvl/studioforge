package mcp

import (
	"sort"
	"slices"
	"strings"
	"testing"
)

func TestAllowedToolsAreScopedByPermissionProfile(t *testing.T) {
	has := func(tools []string, name string) bool {
		for _, tool := range tools {
			if tool == ToolPrefix+name {
				return true
			}
		}
		return false
	}
	readOnly := AllowedTools("read-only")
	write := AllowedTools("workspace-write")
	full := AllowedTools("danger-full-access")

	// execute_luau runs arbitrary Luau in the user's place and multi_edit rewrites
	// scripts; neither may be auto-approved for a read-only agent.
	for _, mutating := range []string{"execute_luau", "multi_edit", "insert_asset", "start_stop_play"} {
		if has(readOnly, mutating) {
			t.Errorf("read-only must not auto-approve %q", mutating)
		}
		if !has(write, mutating) {
			t.Errorf("workspace-write must auto-approve %q", mutating)
		}
	}
	for _, reading := range []string{"script_read", "get_studio_state", "list_roblox_studios"} {
		if !has(readOnly, reading) {
			t.Errorf("read-only must auto-approve %q", reading)
		}
	}
	// These reach past the local place (Marketplace, arbitrary HTTP, synthetic
	// input to the user's desktop), so they stay behind danger-full-access.
	for _, reaching := range []string{"upload_image", "http_get", "user_keyboard_input", "user_mouse_input", "store_image"} {
		if has(write, reaching) {
			t.Errorf("workspace-write must not auto-approve %q", reaching)
		}
		if !has(full, reaching) {
			t.Errorf("danger-full-access must auto-approve %q", reaching)
		}
	}
	// Every Studio tool plus StudioForge's own question, which is not a Studio
	// tool and is granted on every profile.
	if len(full) != len(OfficialTools)+1 {
		t.Errorf("danger-full-access should cover every official tool and the question: got %d want %d", len(full), len(OfficialTools)+1)
	}
	for _, profile := range []string{"read-only", "workspace-write", "danger-full-access"} {
		if !slices.Contains(AllowedTools(profile), QuestionToolFullName) {
			t.Errorf("%s must be able to ask the operator a question", profile)
		}
	}
	if got := AllowedTools("nonsense"); len(got) != 0 {
		t.Errorf("an unknown profile must grant nothing, got %q", got)
	}
}

// Guards against a tool being added to OfficialTools but forgotten in the risk
// tiers: it would silently never be auto-approved and would simply stop working.
func TestEveryOfficialToolIsClassified(t *testing.T) {
	official := append([]string(nil), OfficialTools...)
	// The question is StudioForge's own tool on its own server, not one of
	// Studio's, so it is not what this guard is about.
	var classified []string
	for _, name := range AllowedTools("danger-full-access") {
		if name == QuestionToolFullName {
			continue
		}
		classified = append(classified, strings.TrimPrefix(name, ToolPrefix))
	}
	sort.Strings(official)
	sort.Strings(classified)
	if strings.Join(official, ",") != strings.Join(classified, ",") {
		t.Errorf("risk tiers drifted from OfficialTools:\n official   = %v\n classified = %v", official, classified)
	}
}

func TestAllowedToolsUseServerPrefix(t *testing.T) {
	for _, tool := range AllowedTools("read-only") {
		if tool == QuestionToolFullName {
			// StudioForge's own server, deliberately not Studio's.
			continue
		}
		if !strings.HasPrefix(tool, "mcp__"+ServerName+"__") {
			t.Errorf("tool %q lacks the MCP server prefix", tool)
		}
	}
}
