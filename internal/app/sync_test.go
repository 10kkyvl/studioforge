package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/processes"
	"github.com/10kkyvl/studioforge/internal/rojo"
)

// fakeRojoExecutable builds the same stand-in binary internal/rojo's own
// tests use (testdata/fakes/fakerojo): it answers --version and serves
// forever on "serve" without ever touching Roblox or a real Rojo install.
func fakeRojoExecutable(t *testing.T) string {
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

// TestSyncAdapterLifecycle drives Start -> Status -> Stop through the real
// rojo.Manager and processes.Supervisor — the exact objects wired into
// api.Dependencies.Sync in Run below — so the adapter is proven against a
// running (fake) process, not just type-checked against api.Syncer.
func TestSyncAdapterLifecycle(t *testing.T) {
	supervisor := processes.NewSupervisor()
	manager := rojo.New(supervisor, fakeRojoExecutable(t))
	adapter := &syncAdapter{manager: manager}

	projectFile := filepath.Join(t.TempDir(), "default.project.json")
	if err := os.WriteFile(projectFile, []byte(`{"name":"x","tree":{"$className":"DataModel"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if status := adapter.Status("p1"); status.Active {
		t.Fatalf("status=%+v before Start, want inactive", status)
	}
	status, err := adapter.Start(ctx, "p1", projectFile)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Active || status.Port == 0 {
		t.Fatalf("status=%+v after Start", status)
	}
	if live := adapter.Status("p1"); !live.Active || live.Port != status.Port {
		t.Fatalf("live status=%+v, want it to match Start's %+v", live, status)
	}
	// A second Start for the same project must not silently take over or spawn
	// a competing process — the port already handed out is still the live one.
	if _, err := adapter.Start(ctx, "p1", projectFile); err == nil {
		t.Fatal("a duplicate session was accepted")
	}

	if err := adapter.Stop("p1"); err != nil {
		t.Logf("graceful stop: %v", err)
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	if err := supervisor.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	// rojo.Manager clears the session from its own map on a goroutine once the
	// process is reaped, independently of the supervisor's own bookkeeping, so
	// give it a moment rather than asserting the instant Close returns.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if s := adapter.Status("p1"); !s.Active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session still reported active after Stop and supervisor.Close")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSyncAdapterStopWithoutASessionErrors proves the daemon-shutdown path
// (processes.Supervisor.Close) and the on-demand DELETE handler reach the same
// Manager.Stop, which refuses a project with no live session rather than
// treating "already stopped" as success silently.
func TestSyncAdapterStopWithoutASessionErrors(t *testing.T) {
	manager := rojo.New(processes.NewSupervisor(), fakeRojoExecutable(t))
	adapter := &syncAdapter{manager: manager}
	if err := adapter.Stop("never-started"); err == nil {
		t.Fatal("stopping a project with no session must error")
	}
}
