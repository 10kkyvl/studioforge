package agenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func writeTools(opts Options) []Tool {
	return []Tool{
		createFileTool(opts),
		replaceExactTextTool(opts),
		applyPatchTool(opts),
		createDirTool(opts),
	}
}

type createFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func createFileTool(opts Options) Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to the project root."},"content":{"type":"string","description":"Full file content to write."}},"required":["path","content"]}`)
	return &funcTool{
		name:        "create_file",
		description: "Create or overwrite a file with the given content, creating parent directories as needed.",
		schema:      schema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			var a createFileArgs
			if err := parseArgs(raw, &a); err != nil {
				return errResult("invalid arguments: %v", err)
			}
			if strings.TrimSpace(a.Path) == "" {
				return errResult("path is required")
			}
			resolved, err := opts.Workspace.Resolve(a.Path)
			if err != nil {
				return errResult("%v", err)
			}
			if info, statErr := os.Stat(resolved); statErr == nil && info.IsDir() {
				return errResult("path is a directory: %s", a.Path)
			}
			if err := atomicWriteFile(resolved, []byte(a.Content)); err != nil {
				return errResult("create file: %v", err)
			}
			return Result{Content: fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path)}
		},
	}
}

type replaceExactTextArgs struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

func replaceExactTextTool(opts Options) Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to the project root."},"old_text":{"type":"string","description":"Exact text that must appear exactly once in the file."},"new_text":{"type":"string","description":"Replacement text."}},"required":["path","old_text","new_text"]}`)
	return &funcTool{
		name:        "replace_exact_text",
		description: "Replace the unique occurrence of old_text with new_text in a file.",
		schema:      schema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			var a replaceExactTextArgs
			if err := parseArgs(raw, &a); err != nil {
				return errResult("invalid arguments: %v", err)
			}
			if strings.TrimSpace(a.Path) == "" {
				return errResult("path is required")
			}
			if a.OldText == "" {
				return errResult("old_text is required")
			}
			resolved, err := opts.Workspace.Resolve(a.Path)
			if err != nil {
				return errResult("%v", err)
			}
			data, err := os.ReadFile(resolved)
			if err != nil {
				return errResult("read file: %v", err)
			}
			updated, rerr := replaceUnique(string(data), a.OldText, a.NewText)
			if rerr != nil {
				return errResult("%v", rerr)
			}
			if err := atomicWriteFile(resolved, []byte(updated)); err != nil {
				return errResult("write file: %v", err)
			}
			return Result{Content: fmt.Sprintf("replaced text in %s", a.Path)}
		},
	}
}

type patchEdit struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

type applyPatchArgs struct {
	Edits []patchEdit `json:"edits"`
}

type preparedEdit struct {
	resolved string
	content  string
}

func applyPatchTool(opts Options) Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"edits":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"old_text":{"type":"string"},"new_text":{"type":"string"}},"required":["path","old_text","new_text"]}}},"required":["edits"]}`)
	return &funcTool{
		name:        "apply_patch",
		description: "Apply a set of exact-text replacements across one or more files, all or nothing.",
		schema:      schema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			var a applyPatchArgs
			if err := parseArgs(raw, &a); err != nil {
				return errResult("invalid arguments: %v", err)
			}
			if len(a.Edits) == 0 {
				return errResult("edits is required and must not be empty")
			}
			byPath := map[string]*preparedEdit{}
			var order []string
			for i, e := range a.Edits {
				if strings.TrimSpace(e.Path) == "" {
					return errResult("edit %d: path is required", i)
				}
				if e.OldText == "" {
					return errResult("edit %d: old_text is required", i)
				}
				resolved, err := opts.Workspace.Resolve(e.Path)
				if err != nil {
					return errResult("edit %d (%s): %v", i, e.Path, err)
				}
				key := resolved
				if runtime.GOOS != "linux" {
					key = strings.ToLower(resolved)
				}
				p, ok := byPath[key]
				if !ok {
					data, rerr := os.ReadFile(resolved)
					if rerr != nil {
						return errResult("edit %d (%s): read file: %v", i, e.Path, rerr)
					}
					p = &preparedEdit{resolved: resolved, content: string(data)}
					byPath[key] = p
					order = append(order, key)
				}
				updated, rerr := replaceUnique(p.content, e.OldText, e.NewText)
				if rerr != nil {
					return errResult("edit %d (%s): %v", i, e.Path, rerr)
				}
				p.content = updated
			}
			for _, key := range order {
				p := byPath[key]
				if err := atomicWriteFile(p.resolved, []byte(p.content)); err != nil {
					return errResult("apply patch: write %s: %v", p.resolved, err)
				}
			}
			changed := make([]string, 0, len(order))
			for _, key := range order {
				resolved := byPath[key].resolved
				rel, err := filepath.Rel(opts.Workspace.Root(), resolved)
				if err != nil {
					rel = resolved
				}
				changed = append(changed, filepath.ToSlash(rel))
			}
			return Result{Content: "changed files: " + strings.Join(changed, ", ")}
		},
	}
}

type createDirArgs struct {
	Path string `json:"path"`
}

func createDirTool(opts Options) Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory path relative to the project root."}},"required":["path"]}`)
	return &funcTool{
		name:        "create_dir",
		description: "Create a directory (and any missing parents) within the project.",
		schema:      schema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			var a createDirArgs
			if err := parseArgs(raw, &a); err != nil {
				return errResult("invalid arguments: %v", err)
			}
			if strings.TrimSpace(a.Path) == "" {
				return errResult("path is required")
			}
			resolved, err := opts.Workspace.Resolve(a.Path)
			if err != nil {
				return errResult("%v", err)
			}
			if info, statErr := os.Stat(resolved); statErr == nil && !info.IsDir() {
				return errResult("path exists and is not a directory: %s", a.Path)
			}
			if err := os.MkdirAll(resolved, 0o755); err != nil {
				return errResult("create dir: %v", err)
			}
			return Result{Content: fmt.Sprintf("created directory %s", a.Path)}
		},
	}
}
