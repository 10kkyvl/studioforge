package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/10kkyvl/studioforge/internal/processes"
	"github.com/10kkyvl/studioforge/internal/providers"
)

type Provider struct {
	Executable string
	mu         sync.Mutex
	caps       map[string]bool
	runs       map[string]*handle
}

func New(executable string) *Provider {
	if executable == "" {
		executable = "claude"
	}
	return &Provider{Executable: executable, runs: map[string]*handle{}}
}

func (p *Provider) SetExecutable(executable string) {
	if strings.TrimSpace(executable) == "" {
		executable = "claude"
	}
	p.mu.Lock()
	p.Executable = executable
	p.caps = nil
	p.mu.Unlock()
}

func (p *Provider) executablePath() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Executable
}

func (p *Provider) Diagnose(ctx context.Context) providers.Diagnostics {
	path, err := exec.LookPath(p.executablePath())
	if err != nil {
		return providers.Diagnostics{Message: "Claude Code was not found. Install it, then run StudioForge doctor. Mock mode remains available.", Capabilities: map[string]bool{}}
	}
	versionOut, versionErr := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	helpOut, helpErr := exec.CommandContext(ctx, path, "--help").CombinedOutput()
	caps := parseCapabilities(string(helpOut))
	authOut, authErr := exec.CommandContext(ctx, path, "auth", "status").CombinedOutput()
	authenticated := authErr == nil && authLooksValid(string(authOut))
	message := "Claude Code detected"
	if versionErr != nil {
		message = "Claude Code version check failed"
	}
	if helpErr != nil {
		message += "; capability help unavailable"
	}
	if !authenticated {
		message += "; authentication is not ready"
	}
	p.mu.Lock()
	p.caps = caps
	p.mu.Unlock()
	return providers.Diagnostics{Available: versionErr == nil, Authenticated: authenticated, Version: strings.TrimSpace(string(versionOut)), Path: path, Capabilities: caps, Message: message}
}
func authLooksValid(out string) bool {
	lower := strings.ToLower(out)
	return (strings.Contains(lower, "logged in") || strings.Contains(lower, "authenticated") || strings.Contains(lower, `"loggedin": true`) || strings.Contains(lower, `"loggedin":true`)) && !strings.Contains(lower, "not logged")
}
func parseCapabilities(help string) map[string]bool {
	flags := map[string]string{"stream-json": "stream-json", "partial-messages": "--include-partial-messages", "session-id": "--session-id", "resume": "--resume", "model": "--model", "effort": "--effort", "max-turns": "--max-turns", "max-budget": "--max-budget-usd", "mcp-config": "--mcp-config", "strict-mcp": "--strict-mcp-config", "permission-mode": "--permission-mode", "allowed-tools": "--allowedTools", "denied-tools": "--disallowedTools", "json-schema": "--json-schema", "name": "--name", "append-system-prompt": "--append-system-prompt", "agents": "--agents", "forward-subagent-text": "--forward-subagent-text"}
	out := map[string]bool{}
	for cap, needle := range flags {
		out[cap] = strings.Contains(help, needle)
	}
	return out
}

func (p *Provider) Start(ctx context.Context, req providers.RunRequest) (providers.RunHandle, error) {
	return p.start(ctx, req, "")
}
func (p *Provider) Resume(ctx context.Context, req providers.ResumeRequest) (providers.RunHandle, error) {
	if req.SessionID == "" {
		return nil, errors.New("Claude resume requires a session ID")
	}
	return p.start(ctx, req.RunRequest, req.SessionID)
}
func (p *Provider) start(ctx context.Context, req providers.RunRequest, resume string) (providers.RunHandle, error) {
	diag := p.Diagnose(ctx)
	if !diag.Available {
		return nil, errors.New(diag.Message)
	}
	args := buildArgs(req, resume, diag.Capabilities)
	cmd := exec.CommandContext(ctx, diag.Path, args...)
	cmd.Dir = req.WorkingDirectory
	cmd.Env = processes.MinimalEnvironment(req.Environment)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	h := &handle{cmd: cmd, events: make(chan providers.Event, 128), done: make(chan struct{}), cancel: func() { _ = cmd.Process.Kill() }}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Claude Code: %w", err)
	}
	p.mu.Lock()
	p.runs[req.RunID] = h
	p.mu.Unlock()
	go h.consume(stdout, stderr, func() { p.mu.Lock(); delete(p.runs, req.RunID); p.mu.Unlock() })
	return h, nil
}
func buildArgs(req providers.RunRequest, resume string, caps map[string]bool) []string {
	args := []string{"-p"}
	if caps["stream-json"] {
		args = append(args, "--output-format", "stream-json")
	}
	args = append(args, "--verbose")
	if caps["partial-messages"] {
		args = append(args, "--include-partial-messages")
	}
	if resume != "" && caps["resume"] {
		args = append(args, "--resume", resume)
	} else if caps["session-id"] {
		args = append(args, "--session-id", req.RunID)
	}
	if req.Model != "" && req.Model != "default" && caps["model"] {
		args = append(args, "--model", req.Model)
	}
	if req.Effort != "" && caps["effort"] {
		args = append(args, "--effort", req.Effort)
	}
	if req.MaxTurns > 0 && caps["max-turns"] {
		args = append(args, "--max-turns", strconv.Itoa(req.MaxTurns))
	}
	if req.MaxBudget > 0 && caps["max-budget"] {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(req.MaxBudget, 'f', 2, 64))
	}
	if req.MCPConfigPath != "" && caps["mcp-config"] {
		args = append(args, "--mcp-config", req.MCPConfigPath)
		if caps["strict-mcp"] {
			args = append(args, "--strict-mcp-config")
		}
	}
	if len(req.AllowedTools) > 0 && caps["allowed-tools"] {
		args = append(args, "--allowedTools")
		args = append(args, req.AllowedTools...)
	}
	if caps["permission-mode"] {
		permission := ""
		if req.Mode == "plan" {
			// Plan mode makes the agent propose rather than edit; it overrides the
			// profile-derived permission so PLAN wins regardless of the agent's tier.
			permission = "plan"
		} else {
			switch req.PermissionProfile {
			case "workspace-write":
				// acceptEdits auto-accepts file edits, which a non-interactive
				// (-p) run cannot approve interactively. "default" would block every
				// Write/Edit, so an agent asked to create a file just stalls.
				permission = "acceptEdits"
			case "read-only":
				permission = "default"
			case "danger-full-access":
				permission = "bypassPermissions"
			default:
				permission = req.PermissionProfile
			}
		}
		if permission != "" {
			args = append(args, "--permission-mode", permission)
		}
	}
	if req.SystemPrompt != "" && caps["append-system-prompt"] {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}
	if len(req.Subagents) > 0 && caps["agents"] {
		agents := buildAgentsMap(req.Subagents)
		if encoded, err := json.Marshal(agents); err == nil {
			args = append(args, "--agents", string(encoded))
		}
		if caps["forward-subagent-text"] {
			args = append(args, "--forward-subagent-text")
		}
	}
	// --mcp-config and --allowedTools are variadic, so whichever flag is emitted
	// last would otherwise consume the prompt as one of its values.
	return append(args, "--", req.Prompt)
}

// buildAgentsMap turns the run's subagents into Claude's --agents object,
// keyed by a sanitized name. Names that sanitize to the same slug (or to the
// empty string) would otherwise collide into one key and silently drop
// delegates, so collisions get a numeric suffix and blanks fall back to "agent".
func buildAgentsMap(subagents []providers.Subagent) map[string]map[string]string {
	agents := make(map[string]map[string]string, len(subagents))
	for _, subagent := range subagents {
		key := sanitizeAgentName(subagent.Name)
		if key == "" {
			key = "agent"
		}
		unique := key
		for i := 2; ; i++ {
			if _, exists := agents[unique]; !exists {
				break
			}
			unique = key + "-" + strconv.Itoa(i)
		}
		agents[unique] = map[string]string{
			"description": subagent.Description,
			"prompt":      subagent.Prompt,
		}
	}
	return agents
}

// sanitizeAgentName maps an arbitrary agent name to the lowercase-kebab form
// Claude's --agents keys require: lowercase, non-alphanumeric runs collapse
// to a single hyphen, and leading/trailing hyphens are trimmed.
func sanitizeAgentName(name string) string {
	lower := strings.ToLower(name)
	var b strings.Builder
	lastDash := false
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

type handle struct {
	cmd         *exec.Cmd
	events      chan providers.Event
	done        chan struct{}
	cancel      func()
	once        sync.Once
	mu          sync.RWMutex
	result      providers.Result
	streamError string
	stderr      strings.Builder
}

func (h *handle) consume(stdout, stderr io.Reader, cleanup func()) {
	defer cleanup()
	errCh := make(chan error, 2)
	go func() { errCh <- h.readJSON(stdout) }()
	go func() { errCh <- h.readStderr(stderr) }()
	readErr1 := <-errCh
	readErr2 := <-errCh
	waitErr := h.cmd.Wait()
	result := providers.Result{ExitCode: 0}
	h.mu.RLock()
	result.SessionID = h.result.SessionID
	result.Cost = h.result.Cost
	streamError := h.streamError
	stderrText := h.stderr.String()
	h.mu.RUnlock()
	if h.cmd.ProcessState != nil {
		result.ExitCode = h.cmd.ProcessState.ExitCode()
	}
	if waitErr != nil {
		result.Err = classifyError(waitErr.Error() + " " + streamError + " " + stderrText + " " + errorText(readErr1) + " " + errorText(readErr2))
	} else if streamError != "" {
		result.Err = classifyError(streamError)
	} else if readErr1 != nil && !errors.Is(readErr1, io.EOF) {
		result.Err = readErr1
	}
	h.mu.Lock()
	h.result = result
	h.mu.Unlock()
	close(h.events)
	close(h.done)
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func (h *handle) readJSON(reader io.Reader) error {
	r := bufio.NewReaderSize(reader, 64*1024)
	for {
		line, err := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			event, parseErr := normalize([]byte(line))
			if parseErr != nil {
				event = providers.Event{Type: "error", RawType: "malformed", Payload: map[string]any{"message": "Malformed Claude stream event", "raw": line}}
			}
			event.At = time.Now().UTC()
			if event.SessionID != "" || event.Cost > 0 {
				h.mu.Lock()
				if event.SessionID != "" {
					h.result.SessionID = event.SessionID
				}
				if event.Cost > 0 {
					h.result.Cost = event.Cost
				}
				h.mu.Unlock()
			}
			if event.Error != "" {
				h.mu.Lock()
				h.streamError = event.Error
				h.mu.Unlock()
			}
			h.events <- event
		}
		if err != nil {
			return err
		}
	}
}
func (h *handle) readStderr(reader io.Reader) error {
	r := bufio.NewReader(reader)
	for {
		line, err := r.ReadString('\n')
		if strings.TrimSpace(line) != "" {
			h.mu.Lock()
			_, _ = h.stderr.WriteString(strings.TrimSpace(line) + "\n")
			h.mu.Unlock()
			h.events <- providers.Event{Type: "stderr", RawType: "claude.stderr", Payload: map[string]any{"message": strings.TrimSpace(line)}, At: time.Now().UTC()}
		}
		if err != nil {
			return err
		}
	}
}
func normalize(raw []byte) (providers.Event, error) {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return providers.Event{}, err
	}
	rawType, _ := value["type"].(string)
	event := providers.Event{Type: "message", RawType: rawType, Payload: value}
	switch rawType {
	case "system":
		event.Type = "status"
	case "assistant":
		event.Type = "message"
	case "user":
		event.Type = "tool"
	case "result":
		event.Type = "result"
		if id, ok := value["session_id"].(string); ok {
			event.SessionID = id
		}
		if cost, ok := value["total_cost_usd"].(float64); ok {
			event.Cost = cost
		}
		if failed, _ := value["is_error"].(bool); failed {
			event.Type = "error"
			if message, ok := value["result"].(string); ok && message != "" {
				event.Error = message
			} else {
				event.Error = "Claude returned an error result"
			}
		}
	case "stream_event":
		event.Type = "message"
	default:
		event.Type = "event"
	}
	return event, nil
}
func classifyError(text string) error {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "rate limit"):
		return fmt.Errorf("Claude rate limit: %s", strings.TrimSpace(text))
	case strings.Contains(lower, "auth") || strings.Contains(lower, "login"):
		return fmt.Errorf("Claude authentication error: %s", strings.TrimSpace(text))
	case strings.Contains(lower, "budget"):
		return fmt.Errorf("Claude budget exceeded: %s", strings.TrimSpace(text))
	case strings.Contains(lower, "unknown option") || strings.Contains(lower, "unknown flag"):
		return fmt.Errorf("Claude capability mismatch: %s", strings.TrimSpace(text))
	default:
		return errors.New(strings.TrimSpace(text))
	}
}
func (h *handle) Events() <-chan providers.Event { return h.events }
func (h *handle) Wait() providers.Result {
	<-h.done
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.result
}
func (h *handle) Cancel() error { h.once.Do(h.cancel); return nil }
func (p *Provider) Cancel(_ context.Context, runID string) error {
	p.mu.Lock()
	h, ok := p.runs[runID]
	p.mu.Unlock()
	if !ok {
		return errors.New("Claude run not found")
	}
	return h.Cancel()
}
