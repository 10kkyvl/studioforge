package codex

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/providers"
)

func fakeCodex(t *testing.T) string {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	output := filepath.Join(t.TempDir(), "fakecodex")
	if runtime.GOOS == "windows" {
		output += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", output, filepath.Join(root, "testdata", "fakes", "fakecodex"))
	if body, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake Codex: %v: %s", err, body)
	}
	return output
}

func TestDiagnosticsAndJSONLStream(t *testing.T) {
	p := New(fakeCodex(t))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	diag := p.Diagnose(ctx)
	if !diag.Available || !diag.Authenticated || !diag.Capabilities["jsonl"] {
		t.Fatalf("diagnostics=%+v", diag)
	}
	handle, err := p.Start(ctx, providers.RunRequest{RunID: "run-1", WorkingDirectory: t.TempDir(), Prompt: "test", Model: "gpt-test", Effort: "high", PermissionProfile: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	var messages int
	for event := range handle.Events() {
		if event.Type == "message" {
			messages++
		}
	}
	result := handle.Wait()
	if result.Err != nil || result.SessionID != "fake-thread" || messages == 0 {
		t.Fatalf("result=%+v messages=%d", result, messages)
	}
}

func TestBuildArgsAndNormalization(t *testing.T) {
	args := buildArgs(providers.RunRequest{WorkingDirectory: "root", Prompt: "prompt", Model: "gpt-test", Effort: "high", PermissionProfile: "safe"}, "thread-1")
	expected := []string{"--sandbox", "workspace-write", "--ask-for-approval", "never", "--cd", "root", "--model", "gpt-test", "--config", `model_reasoning_effort="high"`, "exec", "resume", "--json", "--skip-git-repo-check", "thread-1", "prompt"}
	if strings.Join(args, "\x00") != strings.Join(expected, "\x00") {
		t.Fatalf("args=%q want=%q", args, expected)
	}
	event, err := normalize([]byte(`{"type":"item.completed","item":{"type":"agent_message","text":"done"}}`))
	if err != nil || event.Type != "message" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	h := &handle{events: make(chan providers.Event, 1)}
	if err := h.readJSON(strings.NewReader("{bad\n")); !errors.Is(err, io.EOF) {
		t.Fatalf("read error=%v", err)
	}
	if event := <-h.events; event.Type != "error" {
		t.Fatalf("malformed event=%+v", event)
	}
}

func TestBuildArgsAppendsSystemPrompt(t *testing.T) {
	args := buildArgs(providers.RunRequest{WorkingDirectory: "root", Prompt: "prompt", SystemPrompt: "house rules"}, "")
	want := "house rules\n\n---\n\nprompt"
	if got := args[len(args)-1]; got != want {
		t.Fatalf("trailing prompt arg=%q want=%q", got, want)
	}
	// A resumed turn must still carry the system prompt: codex exec has no
	// standing-instructions flag, so dropping it after the first turn would let
	// a long session drift once the operator's own words stop repeating it.
	resumedArgs := buildArgs(providers.RunRequest{WorkingDirectory: "root", Prompt: "prompt", SystemPrompt: "house rules"}, "thread-1")
	if got := resumedArgs[len(resumedArgs)-1]; got != want {
		t.Fatalf("resumed trailing prompt arg=%q want=%q", got, want)
	}
	bareArgs := buildArgs(providers.RunRequest{WorkingDirectory: "root", Prompt: "prompt"}, "")
	if got := bareArgs[len(bareArgs)-1]; got != "prompt" {
		t.Fatalf("trailing prompt arg without system prompt=%q want=%q", got, "prompt")
	}
}

func TestNormalizeParsesUsage(t *testing.T) {
	cases := []struct {
		name string
		line string
		want providers.Usage
	}{
		{
			name: "completed turn reports its tokens",
			line: `{"type":"turn.completed","usage":{"input_tokens":40,"cached_input_tokens":900,"output_tokens":12}}`,
			want: providers.Usage{InputTokens: 40, OutputTokens: 12, CacheReadTokens: 900},
		},
		{
			name: "a turn without usage carries none",
			line: `{"type":"turn.completed"}`,
		},
		{
			name: "other events never carry usage",
			line: `{"type":"item.completed","item":{"type":"agent_message","text":"done"}}`,
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

// Codex reports usage per turn, so a multi-turn run only has a total once the
// reader has summed them, and each event must carry that running total.
func TestReadJSONSumsTurnUsage(t *testing.T) {
	h := &handle{events: make(chan providers.Event, 4)}
	stream := `{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":4}}
{"type":"turn.completed","usage":{"input_tokens":30,"cached_input_tokens":100,"output_tokens":6}}
`
	if err := h.readJSON(strings.NewReader(stream)); !errors.Is(err, io.EOF) {
		t.Fatalf("read error=%v", err)
	}
	close(h.events)
	var last providers.Usage
	for event := range h.events {
		last = event.Usage
	}
	want := providers.Usage{InputTokens: 40, OutputTokens: 10, CacheReadTokens: 100}
	if last != want {
		t.Errorf("streamed total=%+v want %+v", last, want)
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.result.Usage != want {
		t.Errorf("result usage=%+v want %+v", h.result.Usage, want)
	}
}

// Cancelling must actually stop the CLI. It used to kill only the Codex PID,
// which on Windows orphans the MCP shim and any command Codex had started, so
// this exercises the tree-kill path end to end: Wait may not return until the
// process is really gone.
func TestCancelTerminatesRun(t *testing.T) {
	p := New(fakeCodex(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handle, err := p.Start(ctx, providers.RunRequest{RunID: "run-hang", WorkingDirectory: t.TempDir(), Prompt: "test", PermissionProfile: "workspace-write", Environment: []string{"FAKE_CODEX_SCENARIO=hang"}})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range handle.Events() {
		}
	}()
	if err := p.Cancel(ctx, "run-hang"); err != nil {
		t.Fatal(err)
	}
	done := make(chan providers.Result, 1)
	go func() { done <- handle.Wait() }()
	select {
	case <-done:
	case <-time.After(cancelGrace + 10*time.Second):
		t.Fatal("cancelled Codex run never exited")
	}
	// A second Cancel arrives whenever the run context also expires; it must be
	// a no-op rather than signalling a PID the OS may have reused.
	if err := handle.Cancel(); err != nil {
		t.Fatalf("repeat cancel: %v", err)
	}
}
