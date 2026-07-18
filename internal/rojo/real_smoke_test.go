package rojo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/processes"
)

func TestRealRojoSmoke(t *testing.T) {
	if os.Getenv("STUDIOFORGE_REAL_ROJO") != "1" {
		t.Skip("set STUDIOFORGE_REAL_ROJO=1 for a local CLI smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root := t.TempDir()
	project := filepath.Join(root, "default.project.json")
	if err := os.WriteFile(project, []byte(`{"name":"StudioForgeSmoke","tree":{"$className":"DataModel"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	supervisor := processes.NewSupervisor()
	manager := New(supervisor, "")
	diagnostics := manager.Diagnose(ctx)
	if !diagnostics.Available {
		t.Skip(diagnostics.Message)
	}
	session, err := manager.Start(ctx, "smoke", project)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case line, ok := <-session.Lines:
		if !ok {
			t.Fatal("Rojo exited before becoming ready")
		}
		t.Logf("Rojo: %s", line.Text)
	case <-time.After(3 * time.Second):
		t.Fatal("Rojo produced no startup output")
	}
	if err := manager.Stop("smoke"); err != nil {
		t.Logf("graceful stop reported: %v", err)
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	if err := supervisor.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
}
