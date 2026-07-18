package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type LaunchConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}
type Config struct {
	MCPServers map[string]LaunchConfig `json:"mcpServers"`
}

func DetectLauncher(override string) (LaunchConfig, error) {
	if override != "" {
		if _, err := os.Stat(override); err != nil {
			return LaunchConfig{}, fmt.Errorf("Studio MCP override: %w", err)
		}
		if runtime.GOOS == "windows" && (strings.EqualFold(filepath.Ext(override), ".bat") || strings.EqualFold(filepath.Ext(override), ".cmd")) {
			return LaunchConfig{Command: "cmd.exe", Args: []string{"/c", override}}, nil
		}
		return LaunchConfig{Command: override}, nil
	}
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return LaunchConfig{}, errors.New("LOCALAPPDATA is not set")
		}
		launcher := filepath.Join(base, "Roblox", "mcp.bat")
		if _, err := os.Stat(launcher); err != nil {
			return LaunchConfig{}, fmt.Errorf("Studio MCP launcher not found at %s; enable Studio MCP in Assistant settings", launcher)
		}
		return LaunchConfig{Command: "cmd.exe", Args: []string{"/c", launcher}}, nil
	}
	if runtime.GOOS == "darwin" {
		launcher := "/Applications/RobloxStudio.app/Contents/MacOS/StudioMCP"
		if _, err := os.Stat(launcher); err != nil {
			return LaunchConfig{}, fmt.Errorf("Studio MCP launcher not found at %s; install or update Roblox Studio", launcher)
		}
		return LaunchConfig{Command: launcher}, nil
	}
	return LaunchConfig{}, errors.New("Roblox Studio MCP is supported on Windows and macOS")
}

// ServerName is the MCP server key written into the generated config. Claude
// Code derives tool names from it, so the allowlist prefix must track it.
const ServerName = "Roblox_Studio"

// ToolPrefix is how Claude Code namespaces this server's tools.
const ToolPrefix = "mcp__" + ServerName + "__"

// Registering the server is not enough: in non-interactive mode an unapproved
// tool call is denied, so Studio tools must also be auto-approved by name. The
// tiers below mirror the agent permission profiles validated by the API.
var (
	// Observe the place without changing it.
	readOnlyTools = []string{"script_read", "script_search", "script_grep", "search_game_tree", "inspect_instance", "get_studio_state", "get_console_output", "screen_capture", "list_roblox_studios", "set_active_studio"}
	// Change the place, but stay inside it.
	workspaceTools = []string{"multi_edit", "execute_luau", "generate_mesh", "generate_material", "generate_procedural_model", "insert_asset", "search_asset", "wait_job_finished", "start_stop_play", "subagent", "skill", "character_navigation"}
	// Reach beyond the place: Marketplace uploads, arbitrary HTTP, and synthetic
	// input delivered to the user's desktop.
	reachingTools = []string{"upload_image", "store_image", "http_get", "user_keyboard_input", "user_mouse_input"}
)

// AllowedTools returns the Studio tools a run may auto-approve under the given
// agent permission profile, prefixed for Claude Code. An unknown profile grants
// nothing, so a typo denies access rather than widening it.
func AllowedTools(permissionProfile string) []string {
	var names []string
	switch permissionProfile {
	case "read-only":
		names = readOnlyTools
	case "workspace-write":
		names = concat(readOnlyTools, workspaceTools)
	case "danger-full-access":
		names = concat(readOnlyTools, workspaceTools, reachingTools)
	default:
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, ToolPrefix+name)
	}
	return out
}

func concat(groups ...[]string) []string {
	var out []string
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

func WriteConfig(path string, launch LaunchConfig) error {
	body, err := json.MarshalIndent(Config{MCPServers: map[string]LaunchConfig{ServerName: launch}}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o600)
}

var OfficialTools = []string{"script_read", "multi_edit", "script_search", "script_grep", "generate_mesh", "generate_material", "generate_procedural_model", "wait_job_finished", "search_asset", "insert_asset", "upload_image", "store_image", "subagent", "search_game_tree", "inspect_instance", "execute_luau", "get_studio_state", "start_stop_play", "get_console_output", "screen_capture", "character_navigation", "user_keyboard_input", "user_mouse_input", "http_get", "skill", "list_roblox_studios", "set_active_studio"}
