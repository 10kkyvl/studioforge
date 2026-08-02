package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/10kkyvl/studioforge/internal/questions"
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

// QuestionServerName is StudioForge's own MCP server, distinct from the Studio
// passthrough above. It carries one tool — the operator question — and is
// registered on every Claude run, including runs that were never granted
// Studio.
//
// It is separate rather than folded into the Studio server for the reason that
// matters: the Studio server exists only when a run holds a Studio grant, and a
// question is not a Studio concern. Registering the question on that server
// would leave every run without Studio falling back to the text fence, which is
// the failure mode this replaces.
const QuestionServerName = "studioforge"

// QuestionToolFullName is the operator-question tool as Claude Code namespaces
// it — the form the allowlist and the run's own event stream both use.
const QuestionToolFullName = "mcp__" + QuestionServerName + "__" + questions.ToolName

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
	out := make([]string, 0, len(names)+1)
	for _, name := range names {
		out = append(out, ToolPrefix+name)
	}
	// Asking the operator changes nothing in the project, so it is granted on
	// every profile that gets anything at all — including read-only, which can hit
	// a genuine fork in the road like any other run.
	return append(out, QuestionToolFullName)
}

// mutatingTools are the workspaceTools that actually change the open place,
// as opposed to observing it or merely driving its mode. Kept as its own set
// rather than reusing workspaceTools wholesale because a few of that tier do
// not themselves edit anything:
//   - search_asset only queries the Marketplace; insert_asset is the step
//     that actually places something into the world.
//   - wait_job_finished polls a job some earlier call already started, so
//     whatever started it is what gets classified.
//   - start_stop_play changes Play/Stop mode, not the place's contents, and
//     the daemon's own post-run validation loop calls it on every validated
//     run (validator.go's playtest step) — treating it as a mutation would
//     flag every one of those runs regardless of what the agent actually did.
//
// subagent and skill are included deliberately, not by omission: both hand
// work to Roblox's own agent running inside Studio, and that nested agent's
// own tool calls never surface in this provider's event stream — StudioForge
// only ever sees the subagent/skill call itself. Excluding them here would
// silently reproduce the exact miss this issue exists to fix.
//
// This is a "the capability was used" signal, not proof that anything
// changed — an execute_luau call that only reads state still flags. That
// bias is intentional: a false warning costs the operator one sentence,
// while a missed one costs them the truth about what they are looking at.
var mutatingTools = map[string]bool{
	"multi_edit":                true,
	"execute_luau":              true,
	"generate_mesh":             true,
	"generate_material":         true,
	"generate_procedural_model": true,
	"insert_asset":              true,
	"character_navigation":      true,
	"subagent":                  true,
	"skill":                     true,
}

// MutatesPlace reports whether tool is one that changes the place currently
// open in Roblox Studio. tool may be given bare (as it appears in
// OfficialTools) or prefixed with ToolPrefix (as it appears on a run's own
// event stream); both forms are accepted so a caller never has to know which
// shape it is holding.
func MutatesPlace(tool string) bool {
	return mutatingTools[strings.TrimPrefix(tool, ToolPrefix)]
}

func concat(groups ...[]string) []string {
	var out []string
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

func WriteConfig(path string, launch LaunchConfig) error {
	return WriteServers(path, map[string]LaunchConfig{ServerName: launch})
}

// WriteServers writes an MCP config naming several servers at once. A run gets
// one config file rather than one per server, because Claude Code builds its
// toolset from what a single --mcp-config names.
func WriteServers(path string, servers map[string]LaunchConfig) error {
	body, err := json.MarshalIndent(Config{MCPServers: servers}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o600)
}

var OfficialTools = []string{"script_read", "multi_edit", "script_search", "script_grep", "generate_mesh", "generate_material", "generate_procedural_model", "wait_job_finished", "search_asset", "insert_asset", "upload_image", "store_image", "subagent", "search_game_tree", "inspect_instance", "execute_luau", "get_studio_state", "start_stop_play", "get_console_output", "screen_capture", "character_navigation", "user_keyboard_input", "user_mouse_input", "http_get", "skill", "list_roblox_studios", "set_active_studio"}
