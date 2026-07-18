package mcp

import (
	"sort"
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
	if len(full) != len(OfficialTools) {
		t.Errorf("danger-full-access should cover every official tool: got %d want %d", len(full), len(OfficialTools))
	}
	if got := AllowedTools("nonsense"); len(got) != 0 {
		t.Errorf("an unknown profile must grant nothing, got %q", got)
	}
}

// Guards against a tool being added to OfficialTools but forgotten in the risk
// tiers: it would silently never be auto-approved and would simply stop working.
func TestEveryOfficialToolIsClassified(t *testing.T) {
	official := append([]string(nil), OfficialTools...)
	classified := AllowedTools("danger-full-access")
	for i, name := range classified {
		classified[i] = strings.TrimPrefix(name, ToolPrefix)
	}
	sort.Strings(official)
	sort.Strings(classified)
	if strings.Join(official, ",") != strings.Join(classified, ",") {
		t.Errorf("risk tiers drifted from OfficialTools:\n official   = %v\n classified = %v", official, classified)
	}
}

func TestAllowedToolsUseServerPrefix(t *testing.T) {
	for _, tool := range AllowedTools("read-only") {
		if !strings.HasPrefix(tool, "mcp__"+ServerName+"__") {
			t.Errorf("tool %q lacks the MCP server prefix", tool)
		}
	}
}
