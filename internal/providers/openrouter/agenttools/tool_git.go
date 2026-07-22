package agenttools

import (
	"context"
	"encoding/json"
)

var emptySchema = json.RawMessage(`{"type":"object","properties":{}}`)

func gitStatusTool(opts Options) Tool {
	return &funcTool{
		name:        "git_status",
		description: "Show short git status with branch info for the project.",
		schema:      emptySchema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			out, err := opts.Git.Status(ctx, opts.Workspace.Root())
			if err != nil {
				return errResult("git status: %v", err)
			}
			if out == "" {
				return Result{Content: "(clean working tree)"}
			}
			return Result{Content: out}
		},
	}
}

func gitDiffTool(opts Options) Tool {
	return &funcTool{
		name:        "git_diff",
		description: "Show unstaged changes as a unified diff, falling back to the diff against HEAD.",
		schema:      emptySchema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			out, err := opts.Git.Diff(ctx, opts.Workspace.Root())
			if err != nil {
				return errResult("git diff: %v", err)
			}
			if out == "" {
				out, err = opts.Git.DiffHead(ctx, opts.Workspace.Root())
				if err != nil {
					return errResult("git diff: %v", err)
				}
			}
			if out == "" {
				return Result{Content: "(no changes)"}
			}
			return Result{Content: out}
		},
	}
}

func reportChangedFilesTool(opts Options) Tool {
	return &funcTool{
		name:        "report_changed_files",
		description: "List files changed in the working tree via git status.",
		schema:      emptySchema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			isRepo, err := opts.Git.Detect(ctx, opts.Workspace.Root())
			if err != nil {
				return errResult("git detect: %v", err)
			}
			if !isRepo {
				return Result{Content: "not a git repository"}
			}
			out, err := opts.Git.Status(ctx, opts.Workspace.Root())
			if err != nil {
				return errResult("git status: %v", err)
			}
			if out == "" {
				return Result{Content: "(clean working tree)"}
			}
			return Result{Content: out}
		},
	}
}
