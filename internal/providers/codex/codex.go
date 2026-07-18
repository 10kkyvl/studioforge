package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/10kkyvl/studioforge/internal/processes"
	"github.com/10kkyvl/studioforge/internal/providers"
)

type Provider struct {
	mu         sync.RWMutex
	executable string
	runs       map[string]*handle
}

func New(executable string) *Provider {
	if strings.TrimSpace(executable) == "" {
		executable = "codex"
	}
	return &Provider{executable: executable, runs: map[string]*handle{}}
}

func (p *Provider) SetExecutable(executable string) {
	if strings.TrimSpace(executable) == "" {
		executable = "codex"
	}
	p.mu.Lock()
	p.executable = executable
	p.mu.Unlock()
}

func (p *Provider) executablePath() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.executable
}

func (p *Provider) Diagnose(ctx context.Context) providers.Diagnostics {
	executable := p.executablePath()
	path, err := exec.LookPath(executable)
	if err != nil {
		return providers.Diagnostics{Message: "Codex CLI was not found. Install Codex CLI or configure its executable path in Settings.", Capabilities: map[string]bool{}}
	}
	versionOut, versionErr := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if versionErr != nil {
		message := strings.TrimSpace(string(versionOut))
		if message == "" {
			message = versionErr.Error()
		}
		return providers.Diagnostics{Path: path, Message: "Codex CLI could not be started: " + message, Capabilities: map[string]bool{}}
	}
	authOut, authErr := exec.CommandContext(ctx, path, "login", "status").CombinedOutput()
	authenticated := authErr == nil && codexAuthLooksValid(string(authOut))
	message := "Codex CLI detected"
	if !authenticated {
		message += "; run `codex login` before starting agents"
	}
	return providers.Diagnostics{
		Available:     true,
		Authenticated: authenticated,
		Version:       strings.TrimSpace(string(versionOut)),
		Path:          path,
		Capabilities:  map[string]bool{"jsonl": true, "resume": true, "workspace-write": true},
		Message:       message,
	}
}

func codexAuthLooksValid(out string) bool {
	lower := strings.ToLower(out)
	return !strings.Contains(lower, "not logged") &&
		(strings.Contains(lower, "logged in") || strings.Contains(lower, "authenticated"))
}

func (p *Provider) Start(ctx context.Context, req providers.RunRequest) (providers.RunHandle, error) {
	return p.start(ctx, req, "")
}

func (p *Provider) Resume(ctx context.Context, req providers.ResumeRequest) (providers.RunHandle, error) {
	if req.SessionID == "" {
		return nil, errors.New("Codex resume requires a thread ID")
	}
	return p.start(ctx, req.RunRequest, req.SessionID)
}

func (p *Provider) start(ctx context.Context, req providers.RunRequest, resume string) (providers.RunHandle, error) {
	diag := p.Diagnose(ctx)
	if !diag.Available {
		return nil, errors.New(diag.Message)
	}
	if !diag.Authenticated {
		return nil, errors.New("Codex CLI is not authenticated; run `codex login` and recheck diagnostics")
	}
	args := buildArgs(req, resume)
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
	h := &handle{cmd: cmd, events: make(chan providers.Event, 256), done: make(chan struct{})}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Codex CLI: %w", err)
	}
	p.mu.Lock()
	p.runs[req.RunID] = h
	p.mu.Unlock()
	go h.consume(stdout, stderr, func() {
		p.mu.Lock()
		delete(p.runs, req.RunID)
		p.mu.Unlock()
	})
	return h, nil
}

func buildArgs(req providers.RunRequest, resume string) []string {
	// Approval, sandbox, cwd, model, and config are top-level Codex flags. In
	// current CLIs, placing --ask-for-approval after `exec` is rejected. Resume
	// is an exec subcommand and therefore receives its own JSON/Git flags.
	args := []string{"--sandbox", codexSandbox(req.PermissionProfile), "--ask-for-approval", "never"}
	if req.WorkingDirectory != "" {
		args = append(args, "--cd", req.WorkingDirectory)
	}
	if req.Model != "" && req.Model != "default" {
		args = append(args, "--model", req.Model)
	}
	if req.Effort != "" && req.Effort != "default" {
		args = append(args, "--config", fmt.Sprintf("model_reasoning_effort=%q", req.Effort))
	}
	args = append(args, "exec")
	if resume != "" {
		args = append(args, "resume", "--json", "--skip-git-repo-check", resume)
	} else {
		args = append(args, "--json", "--skip-git-repo-check")
	}
	return append(args, req.Prompt)
}

func codexSandbox(permission string) string {
	switch permission {
	case "read-only", "workspace-write", "danger-full-access":
		return permission
	default:
		return "workspace-write"
	}
}

type handle struct {
	cmd         *exec.Cmd
	events      chan providers.Event
	done        chan struct{}
	once        sync.Once
	mu          sync.RWMutex
	result      providers.Result
	stderr      strings.Builder
	streamError string
}

func (h *handle) consume(stdout, stderr io.Reader, cleanup func()) {
	defer cleanup()
	errCh := make(chan error, 2)
	go func() { errCh <- h.readJSON(stdout) }()
	go func() { errCh <- h.readStderr(stderr) }()
	readErr1, readErr2 := <-errCh, <-errCh
	waitErr := h.cmd.Wait()
	h.mu.RLock()
	result := h.result
	stderrText := h.stderr.String()
	streamError := h.streamError
	h.mu.RUnlock()
	if h.cmd.ProcessState != nil {
		result.ExitCode = h.cmd.ProcessState.ExitCode()
	}
	if waitErr != nil {
		result.Err = classifyError(strings.Join([]string{waitErr.Error(), stderrText, errorText(readErr1), errorText(readErr2)}, " "))
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

func (h *handle) readJSON(reader io.Reader) error {
	r := bufio.NewReaderSize(reader, 64*1024)
	for {
		line, err := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			event, parseErr := normalize([]byte(line))
			if parseErr != nil {
				event = providers.Event{Type: "error", RawType: "codex.malformed", Payload: map[string]any{"message": "Malformed Codex JSONL event", "raw": line}}
			}
			event.At = time.Now().UTC()
			if event.SessionID != "" {
				h.mu.Lock()
				h.result.SessionID = event.SessionID
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
		line = strings.TrimSpace(line)
		if line != "" {
			h.mu.Lock()
			_, _ = h.stderr.WriteString(line + "\n")
			h.mu.Unlock()
			h.events <- providers.Event{Type: "stderr", RawType: "codex.stderr", Payload: map[string]any{"message": line}, At: time.Now().UTC()}
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
	event := providers.Event{Type: "event", RawType: rawType, Payload: value}
	switch rawType {
	case "thread.started":
		event.Type = "status"
		event.SessionID, _ = value["thread_id"].(string)
	case "turn.started":
		event.Type = "status"
	case "turn.completed":
		event.Type = "usage"
	case "turn.failed", "error":
		event.Type = "error"
		if message, ok := value["message"].(string); ok {
			event.Error = message
		} else if failure, ok := value["error"].(map[string]any); ok {
			event.Error, _ = failure["message"].(string)
		}
		if event.Error == "" {
			event.Error = "Codex turn failed"
		}
	case "item.started", "item.updated", "item.completed":
		item, _ := value["item"].(map[string]any)
		itemType, _ := item["type"].(string)
		switch itemType {
		case "agent_message", "reasoning":
			event.Type = "message"
		case "file_change":
			event.Type = "artifact"
		case "command_execution", "mcp_tool_call", "web_search":
			event.Type = "tool"
		}
	}
	return event, nil
}

func classifyError(text string) error {
	message := strings.TrimSpace(text)
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "rate limit") || strings.Contains(lower, "usage limit"):
		return fmt.Errorf("Codex usage limit: %s", message)
	case strings.Contains(lower, "auth") || strings.Contains(lower, "login") || strings.Contains(lower, "unauthorized"):
		return fmt.Errorf("Codex authentication error: %s", message)
	case strings.Contains(lower, "approval"):
		return fmt.Errorf("Codex approval configuration error: %s", message)
	default:
		return errors.New(message)
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (h *handle) Events() <-chan providers.Event { return h.events }
func (h *handle) Wait() providers.Result {
	<-h.done
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.result
}
func (h *handle) Cancel() error {
	h.once.Do(func() {
		if h.cmd.Process != nil {
			_ = h.cmd.Process.Kill()
		}
	})
	return nil
}

func (p *Provider) Cancel(_ context.Context, runID string) error {
	p.mu.RLock()
	h, ok := p.runs[runID]
	p.mu.RUnlock()
	if !ok {
		return errors.New("Codex run not found")
	}
	return h.Cancel()
}
