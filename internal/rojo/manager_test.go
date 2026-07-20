package rojo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/processes"
)

func fakeRojo(t *testing.T) string {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", ".."))
	output := filepath.Join(t.TempDir(), "fakerojo")
	if runtime.GOOS == "windows" {
		output += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", output, filepath.Join(root, "testdata", "fakes", "fakerojo"))
	if body, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake Rojo: %v: %s", err, body)
	}
	return output
}
func TestPortAllocationAndLifecycle(t *testing.T) {
	first, err := AllocatePort()
	if err != nil {
		t.Fatal(err)
	}
	second, err := AllocatePort()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("ports unexpectedly equal: %d", first)
	}
	supervisor := processes.NewSupervisor()
	manager := New(supervisor, fakeRojo(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	diag := manager.Diagnose(ctx)
	if !diag.Available {
		t.Fatalf("diagnostics=%+v", diag)
	}
	projectFile := filepath.Join(t.TempDir(), "default.project.json")
	if err := os.WriteFile(projectFile, []byte(`{"name":"x","tree":{"$className":"DataModel"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := manager.Start(ctx, "p1", projectFile)
	if err != nil {
		t.Fatal(err)
	}
	if session.Port == 0 || session.PID == 0 {
		t.Fatalf("session=%+v", session)
	}
	if _, err := manager.Start(ctx, "p1", projectFile); err == nil {
		t.Fatal("duplicate Rojo session started")
	}
	if err := manager.Stop("p1"); err != nil {
		t.Logf("graceful stop: %v", err)
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	if err := supervisor.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
}
func TestRejectsNonProjectFile(t *testing.T) {
	manager := New(processes.NewSupervisor(), fakeRojo(t))
	if _, err := manager.Start(context.Background(), "p", "wrong.json"); err == nil {
		t.Fatal("non-Rojo project accepted")
	}
}

// A live session's log output must reach RecentLines so the project Overview
// can show what rojo serve is currently doing, without the operator needing
// to go find the daemon's own debug log.
func TestSessionRecordsRecentLogLines(t *testing.T) {
	supervisor := processes.NewSupervisor()
	manager := New(supervisor, fakeRojo(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	projectFile := filepath.Join(t.TempDir(), "default.project.json")
	if err := os.WriteFile(projectFile, []byte(`{"name":"x","tree":{"$className":"DataModel"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := manager.Start(ctx, "p1", projectFile)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if lines := session.RecentLines(); len(lines) > 0 {
			if !strings.Contains(lines[0], "Rojo server listening") {
				t.Fatalf("lines=%v, want the fake's startup line", lines)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no log lines recorded within the deadline")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The bounded buffer must drop the oldest lines rather than grow without
// bound over a long-lived serve session.
func TestSessionRecentLinesIsBoundedToTheMostRecent(t *testing.T) {
	session := &Session{}
	for i := 0; i < maxRecentLines+10; i++ {
		session.recordLine(processes.Line{Stream: "stdout", Text: fmt.Sprintf("line %d", i)})
	}
	lines := session.RecentLines()
	if len(lines) != maxRecentLines {
		t.Fatalf("lines=%d, want capped at %d", len(lines), maxRecentLines)
	}
	if !strings.Contains(lines[len(lines)-1], fmt.Sprintf("line %d", maxRecentLines+9)) {
		t.Errorf("last line=%q, want the most recent one kept", lines[len(lines)-1])
	}
	if !strings.Contains(lines[0], "line 10") {
		t.Errorf("first line=%q, want the oldest 10 dropped", lines[0])
	}
}

// A session the operator never explicitly stopped must still die when the
// daemon shuts down (processes.Supervisor.Close) — this is the actual
// daemon-shutdown path, distinct from TestPortAllocationAndLifecycle's
// explicit Stop-then-Close sequence.
func TestSessionDiesWithSupervisorCloseWithoutAnExplicitStop(t *testing.T) {
	supervisor := processes.NewSupervisor()
	manager := New(supervisor, fakeRojo(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	projectFile := filepath.Join(t.TempDir(), "default.project.json")
	if err := os.WriteFile(projectFile, []byte(`{"name":"x","tree":{"$className":"DataModel"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(ctx, "p1", projectFile); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.Session("p1"); !ok {
		t.Fatal("session not registered after Start")
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	if err := supervisor.Close(closeCtx); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := manager.Session("p1"); !ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("session still registered after supervisor.Close, without ever being explicitly stopped")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
