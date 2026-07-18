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
