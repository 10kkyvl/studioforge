// Package studiochanges describes dispatched Studio operations, not a snapshot
// or a claim about the resulting DataModel. Arbitrary Luau cannot be inferred.
package studiochanges

import (
	"context"
	"sort"
	"strings"
)

type Change struct {
	ID         string   `json:"id"`
	CallID     string   `json:"callId"`
	RunID      string   `json:"runId"`
	CreatedAt  string   `json:"createdAt"`
	Tool       string   `json:"tool"`
	Target     string   `json:"target"`
	Operation  string   `json:"operation"`
	Properties []string `json:"properties"`
	Status     string   `json:"status"`
}

type Recorder interface {
	Start(context.Context, string, map[string]any) (string, error)
	Finish(context.Context, string, string) error
}

// Unknown tools are conservatively journaled. Explicit read-only tools do not
// generate changes; execute_luau is opaque even when its code only reads.
func Mutating(tool string) bool {
	switch tool {
	case "script_read", "script_search", "script_grep", "search_game_tree", "inspect_instance", "get_studio_state", "get_console_output", "screen_capture", "list_roblox_studios", "set_active_studio", "search_asset", "wait_job_finished", "http_get":
		return false
	default:
		return true
	}
}

// Normalize extracts structural metadata only. Script bodies, property values,
// tool results and free-form instructions are deliberately never stored here.
func Normalize(tool string, args map[string]any) []Change {
	if !Mutating(tool) {
		return nil
	}
	var out []Change
	var walk func(map[string]any, int, string)
	walk = func(a map[string]any, depth int, inheritedTarget string) {
		target := inheritedTarget
		for _, key := range []string{"instance_path", "instancePath", "path", "target", "script_path", "file_path"} {
			if value, ok := a[key].(string); ok && value != "" {
				target = bounded(value)
				break
			}
		}
		if depth > 4 || len(out) >= 500 {
			return
		}
		for _, key := range []string{"edits", "changes", "operations"} {
			if items, ok := a[key].([]any); ok && len(items) > 0 {
				for _, item := range items {
					if obj, ok := item.(map[string]any); ok {
						walk(obj, depth+1, target)
					}
				}
				return
			}
		}
		c := Change{Tool: tool, Operation: "unknown", Properties: []string{}, Status: "pending"}
		c.Target = target
		if tool == "insert_asset" {
			c.Operation = "create"
		}
		if tool == "multi_edit" {
			c.Operation = "modify"
			c.Properties = append(c.Properties, "Source")
		}
		for _, key := range []string{"operation", "action", "type"} {
			value, _ := a[key].(string)
			switch strings.ToLower(value) {
			case "create", "insert", "add":
				c.Operation = "create"
			case "modify", "update", "edit", "set":
				c.Operation = "modify"
			case "delete", "remove", "destroy":
				c.Operation = "delete"
			case "reparent", "move":
				c.Operation = "reparent"
			}
		}
		if props, ok := a["properties"].(map[string]any); ok {
			c.Properties = []string{}
			for key := range props {
				c.Properties = append(c.Properties, bounded(key))
			}
			sort.Strings(c.Properties)
			if len(c.Properties) > 100 {
				c.Properties = c.Properties[:100]
			}
		}
		out = append(out, c)
	}
	walk(args, 0, "")
	if len(out) == 0 {
		out = append(out, Change{Tool: tool, Operation: "unknown", Properties: []string{}, Status: "pending"})
	}
	return out
}

func bounded(value string) string {
	if len(value) > 512 {
		return strings.ToValidUTF8(value[:512], "")
	}
	return value
}
