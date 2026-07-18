package rojo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
