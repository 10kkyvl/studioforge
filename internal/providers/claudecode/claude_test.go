package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/providers"
)

func fakeClaude(t *testing.T) string {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	output := filepath.Join(t.TempDir(), "fakeclaude")
	if runtime.GOOS == "windows" {
		output += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", output, filepath.Join(root, "testdata", "fakes", "fakeclaude"))
	if body, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake Claude: %v: %s", err, body)
	}
	return output
}
func TestCapabilityDetectionAndStream(t *testing.T) {
	path := fakeClaude(t)
	provider := New(path)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	diag := provider.Diagnose(ctx)
	if !diag.Available || !diag.Authenticated || !diag.Capabilities["resume"] || !diag.Capabilities["stream-json"] {
		t.Fatalf("diagnostics=%+v", diag)
	}
	handle, err := provider.Start(ctx, providers.RunRequest{RunID: "run-1", WorkingDirectory: t.TempDir(), Prompt: "test", Model: "balanced", MaxTurns: 3, MaxBudget: 1, PermissionProfile: "default"})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for range handle.Events() {
		count++
	}
	result := handle.Wait()
	if result.Err != nil || result.SessionID != "fake-session" || result.Cost != 0.12 || count < 4 {
		t.Fatalf("result=%+v count=%d", result, count)
	}
}
func TestMalformedAndClassifiedErrors(t *testing.T) {
	if !strings.Contains(classifyError("rate limit exceeded").Error(), "rate limit") {
		t.Fatal("rate limit not classified")
	}
	if !strings.Contains(classifyError("authentication required").Error(), "authentication") {
		t.Fatal("auth not classified")
	}
	if !strings.Contains(classifyError("unknown option --x").Error(), "capability") {
		t.Fatal("flag not classified")
	}
	h := &handle{events: make(chan providers.Event, 2)}
	long := `{"type":"assistant","value":"` + strings.Repeat("x", 2<<20) + `"}` + "\n"
	err := h.readJSON(strings.NewReader(long))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("read error=%v", err)
	}
	event := <-h.events
	if event.RawType != "assistant" {
		t.Fatalf("event=%+v", event)
	}
}

func TestSemanticErrorResult(t *testing.T) {
	event, err := normalize([]byte(`{"type":"result","is_error":true,"result":"organization has disabled access","session_id":"s"}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "error" || event.Error == "" || event.SessionID != "s" {
		t.Fatalf("event=%+v", event)
	}
}
func TestNormalizeParsesUsage(t *testing.T) {
	cases := []struct {
		name string
		line string
		want providers.Usage
	}{
		{
			name: "assistant message reports its own tokens",
			line: `{"type":"assistant","message":{"usage":{"input_tokens":12,"output_tokens":34,"cache_read_input_tokens":56,"cache_creation_input_tokens":78}}}`,
			want: providers.Usage{InputTokens: 12, OutputTokens: 34, CacheReadTokens: 56, CacheCreationTokens: 78},
		},
		{
			name: "result reports the session total",
			line: `{"type":"result","session_id":"s","usage":{"input_tokens":100,"output_tokens":200}}`,
			want: providers.Usage{InputTokens: 100, OutputTokens: 200},
		},
		{
			name: "a run that never cached leaves the cache counters at zero",
			line: `{"type":"result","usage":{"input_tokens":5,"output_tokens":6,"cache_read_input_tokens":null}}`,
			want: providers.Usage{InputTokens: 5, OutputTokens: 6},
		},
		{
			name: "an event without usage carries none",
			line: `{"type":"assistant","message":{"content":[]}}`,
		},
		{
			name: "a malformed usage object is not fatal",
			line: `{"type":"result","usage":"unavailable"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event, err := normalize([]byte(tc.line))
			if err != nil {
				t.Fatal(err)
			}
			if event.Usage != tc.want {
				t.Errorf("usage=%+v want %+v", event.Usage, tc.want)
			}
		})
	}
}

// Per-message reports are deltas and the result is the session total, so the
// stream reader must add the former and let the latter replace the sum — and
// every event must leave carrying the running total, since that is what the
// live progress display renders.
func TestReadJSONAccumulatesUsage(t *testing.T) {
	h := &handle{events: make(chan providers.Event, 8)}
	stream := `{"type":"assistant","message":{"usage":{"input_tokens":10,"output_tokens":5}}}
{"type":"assistant","message":{"usage":{"input_tokens":20,"output_tokens":7}}}
{"type":"result","session_id":"s","total_cost_usd":0.25,"usage":{"input_tokens":30,"output_tokens":12,"cache_read_input_tokens":900}}
`
	if err := h.readJSON(strings.NewReader(stream)); !errors.Is(err, io.EOF) {
		t.Fatalf("read error=%v", err)
	}
	close(h.events)
	var seen []providers.Usage
	for event := range h.events {
		seen = append(seen, event.Usage)
	}
	want := []providers.Usage{
		{InputTokens: 10, OutputTokens: 5},
		{InputTokens: 30, OutputTokens: 12},
		{InputTokens: 30, OutputTokens: 12, CacheReadTokens: 900},
	}
	if len(seen) != len(want) {
		t.Fatalf("events=%d want %d", len(seen), len(want))
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("event %d usage=%+v want %+v", i, seen[i], want[i])
		}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.result.Usage != want[len(want)-1] {
		t.Errorf("result usage=%+v want %+v", h.result.Usage, want[len(want)-1])
	}
}

func TestBuildArgsAddsOnlyCapabilities(t *testing.T) {
	args := buildArgs(providers.RunRequest{RunID: "id", Prompt: "prompt", Model: "balanced", Effort: "high", MaxTurns: 4, MaxBudget: 2, MCPConfigPath: "mcp.json", PermissionProfile: "default"}, "resume-id", map[string]bool{"stream-json": true, "resume": true, "model": true})
	joined := strings.Join(args, " ")
	for _, expected := range []string{"-p", "stream-json", "--resume resume-id", "--model balanced", "prompt"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("missing %q in %s", expected, joined)
		}
	}
	for _, unexpected := range []string{"--effort", "--max-turns", "--max-budget-usd", "--mcp-config", "--permission-mode"} {
		if strings.Contains(joined, unexpected) {
			t.Errorf("unexpected %q in %s", unexpected, joined)
		}
	}
}

// Claude Code declares --mcp-config <configs...> and --allowedTools <tools...>
// as variadic. Whichever flag lands last before the positional prompt swallows
// it, and the run dies with "MCP config file not found: <the user's prompt>".
// Only an explicit -- separator keeps the prompt positional regardless of which
// capability-gated flags happen to be emitted.
func TestBuildArgsKeepsPromptPositional(t *testing.T) {
	for name, caps := range map[string]map[string]bool{
		"mcp-config last":  {"mcp-config": true},
		"no flags at all":  {},
		"strict follows":   {"mcp-config": true, "strict-mcp": true},
		"permission last":  {"mcp-config": true, "permission-mode": true},
		"every capability": {"stream-json": true, "partial-messages": true, "session-id": true, "model": true, "effort": true, "max-budget": true, "mcp-config": true, "strict-mcp": true, "permission-mode": true},
	} {
		t.Run(name, func(t *testing.T) {
			args := buildArgs(providers.RunRequest{RunID: "id", Prompt: "Say OK", Model: "opus", Effort: "high", MaxBudget: 1, MCPConfigPath: "cfg.json", PermissionProfile: "default"}, "", caps)
			if len(args) < 2 {
				t.Fatalf("args too short: %q", args)
			}
			if got := args[len(args)-1]; got != "Say OK" {
				t.Errorf("prompt must be the final argument, got %q in %q", got, args)
			}
			if got := args[len(args)-2]; got != "--" {
				t.Errorf("prompt must be preceded by --, got %q in %q", got, args)
			}
		})
	}
}

// Registering an MCP server does not make its tools callable: in -p mode an
// unapproved tool call is denied, so the Studio tools must be named explicitly.
func TestBuildArgsPassesAllowedTools(t *testing.T) {
	tools := []string{"mcp__Roblox_Studio__script_read", "mcp__Roblox_Studio__multi_edit"}
	caps := map[string]bool{"mcp-config": true, "strict-mcp": true, "allowed-tools": true}
	args := buildArgs(providers.RunRequest{RunID: "id", Prompt: "p", MCPConfigPath: "cfg.json", AllowedTools: tools}, "", caps)

	at := -1
	for i, arg := range args {
		if arg == "--allowedTools" {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("--allowedTools missing from %q", args)
	}
	if got := args[at+1 : at+3]; got[0] != tools[0] || got[1] != tools[1] {
		t.Errorf("tools must follow the flag space-separated, got %q in %q", got, args)
	}
	// Without the capability the flag must not appear at all.
	bare := buildArgs(providers.RunRequest{RunID: "id", Prompt: "p", AllowedTools: tools}, "", map[string]bool{})
	for _, arg := range bare {
		if arg == "--allowedTools" {
			t.Errorf("--allowedTools emitted without the capability: %q", bare)
		}
	}
}

func TestBuildArgsAppendsSystemPrompt(t *testing.T) {
	caps := map[string]bool{"append-system-prompt": true}
	args := buildArgs(providers.RunRequest{RunID: "id", Prompt: "hi", SystemPrompt: "You are the orchestrator."}, "", caps)
	found := false
	for i, a := range args {
		if a == "--append-system-prompt" {
			if i+1 >= len(args) || args[i+1] != "You are the orchestrator." {
				t.Fatalf("--append-system-prompt must be followed by its value, got %v", args)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("system prompt was not passed: %v", args)
	}
	// Without the capability it must be silently dropped, never sent bare.
	bare := buildArgs(providers.RunRequest{RunID: "id", Prompt: "hi", SystemPrompt: "persona"}, "", map[string]bool{})
	for _, a := range bare {
		if a == "--append-system-prompt" || a == "persona" {
			t.Fatalf("system prompt leaked without capability: %v", bare)
		}
	}
}

func TestBuildArgsPlanMode(t *testing.T) {
	caps := map[string]bool{"permission-mode": true}
	permissionAfter := func(args []string) string {
		for i, a := range args {
			if a == "--permission-mode" && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}
	// Plan mode maps to --permission-mode plan and overrides the profile.
	plan := buildArgs(providers.RunRequest{RunID: "id", Prompt: "p", Mode: "plan", PermissionProfile: "workspace-write"}, "", caps)
	if got := permissionAfter(plan); got != "plan" {
		t.Errorf("plan mode must emit --permission-mode plan, got %q in %v", got, plan)
	}
	// Do mode keeps the profile-derived value, never plan. workspace-write maps
	// to acceptEdits so a non-interactive run can actually write files.
	do := buildArgs(providers.RunRequest{RunID: "id", Prompt: "p", Mode: "do", PermissionProfile: "workspace-write"}, "", caps)
	if got := permissionAfter(do); got != "acceptEdits" {
		t.Errorf("workspace-write must map to acceptEdits, got %q in %v", got, do)
	}
	// Empty mode behaves like do.
	empty := buildArgs(providers.RunRequest{RunID: "id", Prompt: "p", PermissionProfile: "danger-full-access"}, "", caps)
	if got := permissionAfter(empty); got != "bypassPermissions" {
		t.Errorf("empty mode must derive from the profile, got %q in %v", got, empty)
	}
	// Without the capability the flag never appears, even in plan mode.
	bare := buildArgs(providers.RunRequest{RunID: "id", Prompt: "p", Mode: "plan", PermissionProfile: "workspace-write"}, "", map[string]bool{})
	if permissionAfter(bare) != "" {
		t.Errorf("permission-mode leaked without the capability: %v", bare)
	}
}

func TestSanitizeAgentName(t *testing.T) {
	cases := map[string]string{
		"QA / Playtester":     "qa-playtester",
		"Gameplay Engineer":   "gameplay-engineer",
		"  --Leading--Dashes": "leading-dashes",
		"already-kebab":       "already-kebab",
	}
	for in, want := range cases {
		if got := sanitizeAgentName(in); got != want {
			t.Errorf("sanitizeAgentName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildArgsEmitsAgents(t *testing.T) {
	subagents := []providers.Subagent{
		{Name: "Gameplay Engineer", Description: "Gameplay Engineer", Prompt: "Build features."},
		{Name: "QA / Playtester", Description: "QA / Playtester", Prompt: "Playtest and report."},
	}
	caps := map[string]bool{"agents": true, "forward-subagent-text": true}
	args := buildArgs(providers.RunRequest{RunID: "id", Prompt: "p", Subagents: subagents}, "", caps)

	at := -1
	for i, a := range args {
		if a == "--agents" {
			at = i
		}
	}
	if at < 0 || at+1 >= len(args) {
		t.Fatalf("--agents missing from %v", args)
	}
	var parsed map[string]map[string]string
	if err := json.Unmarshal([]byte(args[at+1]), &parsed); err != nil {
		t.Fatalf("--agents value is not valid JSON: %v (%q)", err, args[at+1])
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 agents, got %+v", parsed)
	}
	eng, ok := parsed["gameplay-engineer"]
	if !ok {
		t.Fatalf("missing sanitized key gameplay-engineer in %+v", parsed)
	}
	if eng["description"] != "Gameplay Engineer" || eng["prompt"] != "Build features." {
		t.Errorf("gameplay-engineer entry=%+v", eng)
	}
	qa, ok := parsed["qa-playtester"]
	if !ok {
		t.Fatalf("missing sanitized key qa-playtester in %+v", parsed)
	}
	if qa["description"] != "QA / Playtester" || qa["prompt"] != "Playtest and report." {
		t.Errorf("qa-playtester entry=%+v", qa)
	}
	found := false
	for _, a := range args {
		if a == "--forward-subagent-text" {
			found = true
		}
	}
	if !found {
		t.Errorf("--forward-subagent-text missing from %v", args)
	}

	// Without the capabilities, neither flag appears at all.
	bare := buildArgs(providers.RunRequest{RunID: "id", Prompt: "p", Subagents: subagents}, "", map[string]bool{})
	for _, a := range bare {
		if a == "--agents" || a == "--forward-subagent-text" {
			t.Errorf("agent flags leaked without capability: %v", bare)
		}
	}
}

func TestBuildAgentsMapDedupesAndFillsBlanks(t *testing.T) {
	m := buildAgentsMap([]providers.Subagent{
		{Name: "Build Bot", Description: "a", Prompt: "p1"},
		{Name: "build-bot", Description: "b", Prompt: "p2"},
		{Name: "★★★", Description: "c", Prompt: "p3"},
	})
	if len(m) != 3 {
		t.Fatalf("colliding/blank names must not drop delegates, got %d keys: %v", len(m), m)
	}
	if _, ok := m["build-bot"]; !ok {
		t.Errorf("expected a build-bot key: %v", m)
	}
	if _, ok := m["build-bot-2"]; !ok {
		t.Errorf("a colliding name must get a numeric suffix: %v", m)
	}
	if _, ok := m["agent"]; !ok {
		t.Errorf("an all-symbol name must fall back to 'agent': %v", m)
	}
}

func TestFakeClaudeScenarios(t *testing.T) {
	path := fakeClaude(t)
	for _, scenario := range []string{"rate_limit", "auth_error", "budget", "crash"} {
		t.Run(scenario, func(t *testing.T) {
			provider := New(path)
			handle, err := provider.Start(context.Background(), providers.RunRequest{RunID: "run-" + scenario, WorkingDirectory: t.TempDir(), Prompt: "test", Environment: []string{"FAKE_CLAUDE_SCENARIO=" + scenario}})
			if err != nil {
				t.Fatal(err)
			}
			for range handle.Events() {
			}
			result := handle.Wait()
			if result.Err == nil {
				t.Fatal("scenario unexpectedly succeeded")
			}
		})
	}
}
func TestMain(m *testing.M) { os.Exit(m.Run()) }

// Cancelling must actually stop the CLI. It used to kill only the Claude PID,
// which on Windows orphans the MCP shim and any tool process Claude had
// started, so this exercises the tree-kill path end to end: Wait may not return
// until the process is really gone.
func TestCancelTerminatesRun(t *testing.T) {
	provider := New(fakeClaude(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handle, err := provider.Start(ctx, providers.RunRequest{RunID: "run-hang", WorkingDirectory: t.TempDir(), Prompt: "test", PermissionProfile: "default", Environment: []string{"FAKE_CLAUDE_SCENARIO=hang"}})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range handle.Events() {
		}
	}()
	if err := provider.Cancel(ctx, "run-hang"); err != nil {
		t.Fatal(err)
	}
	done := make(chan providers.Result, 1)
	go func() { done <- handle.Wait() }()
	select {
	case <-done:
	case <-time.After(cancelGrace + 10*time.Second):
		t.Fatal("cancelled Claude run never exited")
	}
	// A second Cancel arrives whenever the run context also expires; it must be
	// a no-op rather than signalling a PID the OS may have reused.
	if err := handle.Cancel(); err != nil {
		t.Fatalf("repeat cancel: %v", err)
	}
}
