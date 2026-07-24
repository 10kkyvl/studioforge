package agenttools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/10kkyvl/studioforge/internal/processes"
)

// runCommandAllowlist names the executables a workspace-write run may start.
// npx is deliberately absent: fetching and running an arbitrary package is not
// meaningfully different from running an arbitrary command, which is what the
// danger-full-access profile is for.
//
// This allowlist raises the bar on the obvious escapes below; it is not a
// sandbox. A profile that can both write files and run a build tool can always
// arrange to execute code (a test file, an npm script, a Makefile recipe) —
// see docs/SECURITY.md, which states this plainly rather than implying the
// allowlist contains what a build tool can do.
var runCommandAllowlist = map[string]bool{
	"git": true, "go": true, "gofmt": true, "goimports": true,
	"npm": true, "pnpm": true, "yarn": true, "node": true,
	"rojo": true, "python": true, "python3": true, "pytest": true,
	"make": true, "cargo": true,
}

// inlineEvalFlags turn an allowlisted interpreter into "run this source text",
// which is the one escape worth refusing outright: `node -e`, `python -c` and
// their long forms are arbitrary code with no file on disk and no build step
// in between.
var inlineEvalFlags = map[string]bool{
	"-e": true, "--eval": true, "-c": true, "--command": true,
	"-p": true, "--print": true,
}

// interpreterCommands are the allowlisted executables that accept the inline
// eval flags above.
var interpreterCommands = map[string]bool{
	"node": true, "python": true, "python3": true,
}

// refusedSubcommands are allowlisted executables paired with a subcommand that
// compiles and runs caller-supplied source in one step. `go build`/`go test`
// stay available; `go run <anything>` is the direct equivalent of an inline
// eval and is refused with it.
var refusedSubcommands = map[string]string{"go": "run"}

// checkWorkspaceCommand applies the workspace-write restrictions to an already
// tokenized command line. An empty return means the command may run.
func checkWorkspaceCommand(exe string, argv []string) string {
	base := strings.ToLower(filepath.Base(exe))
	base = strings.TrimSuffix(base, ".exe")
	base = strings.TrimSuffix(base, ".cmd")
	base = strings.TrimSuffix(base, ".bat")
	if !runCommandAllowlist[base] {
		return fmt.Sprintf("command not allowed in workspace-write profile: %s (use danger-full-access for arbitrary commands)", base)
	}
	if interpreterCommands[base] {
		for _, arg := range argv {
			if inlineEvalFlags[strings.ToLower(arg)] {
				return fmt.Sprintf("inline code execution (%s %s) is not allowed in workspace-write profile; write the code to a file in the project and run that instead, or use danger-full-access", base, arg)
			}
		}
	}
	if refused, ok := refusedSubcommands[base]; ok && len(argv) > 0 && strings.ToLower(argv[0]) == refused {
		return fmt.Sprintf("%s %s is not allowed in workspace-write profile; use danger-full-access for it", base, refused)
	}
	return ""
}

type runCommandArgs struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Shell   bool     `json:"shell"`
}

func (s *ToolSet) runCommandTool() Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Executable name, or a full command line to tokenize when args is omitted."},"args":{"type":"array","items":{"type":"string"},"description":"Arguments for the executable named in command."},"shell":{"type":"boolean","description":"danger-full-access only: run command through the platform shell."}},"required":["command"]}`)
	opts := s.opts
	return &funcTool{
		name:        "run_command",
		description: "Run a supervised command inside the project workspace.",
		schema:      schema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			var a runCommandArgs
			if err := parseArgs(raw, &a); err != nil {
				return errResult("invalid arguments: %v", err)
			}
			if strings.TrimSpace(a.Command) == "" {
				return errResult("command is required")
			}
			if a.Shell && s.profile != ProfileDanger {
				return errResult("shell execution is only allowed in danger-full-access profile")
			}
			var exe string
			var argv []string
			if a.Shell {
				exe, argv = platformShell(a.Command)
			} else if len(a.Args) > 0 {
				exe, argv = a.Command, a.Args
			} else {
				tokens := tokenizeCommand(a.Command)
				if len(tokens) == 0 {
					return errResult("command is required")
				}
				exe, argv = tokens[0], tokens[1:]
			}
			if !a.Shell && s.profile != ProfileDanger {
				if refusal := checkWorkspaceCommand(exe, argv); refusal != "" {
					return errResult("%s", refusal)
				}
			}
			id := fmt.Sprintf("%s-cmd-%d", opts.RunID, s.cmdSeq.Add(1))
			proc, err := opts.Supervisor.Start(ctx, processes.Spec{
				ID:               id,
				Kind:             "agent-shell",
				ProjectID:        opts.ProjectID,
				RunID:            opts.RunID,
				Executable:       exe,
				Args:             argv,
				WorkingDirectory: opts.Workspace.Root(),
				Environment:      processes.MinimalEnvironment(nil),
				MaxRuntime:       opts.CommandTimeout,
			})
			if err != nil {
				return errResult("start command: %v", err)
			}
			return runAndCollect(ctx, proc, opts.MaxOutputBytes)
		},
	}
}

func tokenizeCommand(command string) []string {
	var tokens []string
	var cur strings.Builder
	inQuotes := false
	for _, r := range command {
		switch {
		case r == '"':
			inQuotes = !inQuotes
		case unicode.IsSpace(r) && !inQuotes:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func platformShell(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd"
		}
		return comspec, []string{"/c", command}
	}
	return "/bin/sh", []string{"-c", command}
}

func runAndCollect(ctx context.Context, proc *processes.Process, maxOutputBytes int) Result {
	var mu sync.Mutex
	var buf bytes.Buffer
	truncated := false
	linesDone := make(chan struct{})
	go func() {
		defer close(linesDone)
		for line := range proc.Lines() {
			mu.Lock()
			if !truncated {
				remaining := maxOutputBytes - buf.Len()
				if remaining <= 0 {
					truncated = true
				} else {
					text := line.Text
					if len(text) > remaining {
						text = truncateBytes(text, remaining)
						truncated = true
					}
					buf.WriteString(text)
				}
			}
			mu.Unlock()
		}
	}()
	resultCh := make(chan processes.Result, 1)
	go func() { resultCh <- proc.Wait() }()

	var result processes.Result
	cancelled := false
	select {
	case <-ctx.Done():
		_ = proc.Terminate(2 * time.Second)
		result = <-resultCh
		cancelled = true
	case result = <-resultCh:
	}
	<-linesDone

	mu.Lock()
	output := buf.String()
	isTruncated := truncated
	mu.Unlock()

	if isTruncated {
		output += "\n... (output truncated)\n"
	}
	if cancelled {
		output += "\n[cancelled: " + ctx.Err().Error() + "]\n"
		return Result{IsError: true, Content: output}
	}
	output += fmt.Sprintf("\n[exit code %d]", result.ExitCode)
	return Result{IsError: result.ExitCode != 0, Content: output}
}
