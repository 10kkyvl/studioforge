package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadContextGathersConstitutionAndRequirements(t *testing.T) {
	root := t.TempDir()
	agentDir := filepath.Join(root, ".agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constitution.yaml"), []byte("server_authoritative: true"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "requirements.md"), []byte("# Goal\nBuild a lobby"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := LoadContext(root)
	if !strings.Contains(ctx, "server_authoritative") || !strings.Contains(ctx, "Build a lobby") {
		t.Errorf("context should carry both files, got:\n%s", ctx)
	}
}

func TestLoadContextEmptyWhenNoFiles(t *testing.T) {
	if got := LoadContext(t.TempDir()); got != "" {
		t.Errorf("a project without .agent files must yield empty context, got %q", got)
	}
}
