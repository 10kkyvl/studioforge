package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureReferenceWritesTheDocument(t *testing.T) {
	root := t.TempDir()
	if err := EnsureReference(root, ".agent/roblox-ui.md", "reference body"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, ".agent", "roblox-ui.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "reference body" {
		t.Fatalf("wrote %q, want %q", body, "reference body")
	}
}

// The point of shipping this into the project rather than into the prompt is
// that it belongs to the operator afterwards. A run that quietly restored our
// version over their edit would take that back.
func TestEnsureReferenceNeverOverwritesAnEditedDocument(t *testing.T) {
	root := t.TempDir()
	if err := EnsureReference(root, ".agent/roblox-ui.md", "shipped body"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".agent", "roblox-ui.md")
	if err := os.WriteFile(path, []byte("the operator's own rules"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureReference(root, ".agent/roblox-ui.md", "shipped body"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "the operator's own rules" {
		t.Fatalf("the operator's edit was overwritten: %q", body)
	}
}

// A project registered before the document existed still has a .agent/
// directory full of the operator's own files, and one created today may have no
// .agent/ at all. Both have to work.
func TestEnsureReferenceCreatesTheDirectoryWhenMissing(t *testing.T) {
	root := t.TempDir()
	if _, err := os.Stat(filepath.Join(root, ".agent")); !os.IsNotExist(err) {
		t.Fatalf("test setup: .agent should not exist yet, got %v", err)
	}
	if err := EnsureReference(root, ".agent/roblox-ui.md", "reference body"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent", "roblox-ui.md")); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureReferenceLeavesTheContextFilesAlone(t *testing.T) {
	root := t.TempDir()
	agentDir := filepath.Join(root, ".agent")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constitution.yaml"), []byte("architecture:\n  server_authoritative: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureReference(root, ".agent/roblox-ui.md", "reference body"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(agentDir, "constitution.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "architecture:\n  server_authoritative: true\n" {
		t.Fatalf("the constitution was disturbed: %q", body)
	}
}

// The reference is deliberately not one of the two files LoadContext reads: it
// is there to be opened when it is needed, not carried in every prompt. If that
// ever changes, the interface rules would start riding on every run, which is
// exactly what they were scoped to avoid.
func TestReferenceIsNotLoadedAsProjectContext(t *testing.T) {
	root := t.TempDir()
	if err := EnsureReference(root, ".agent/roblox-ui.md", "reference body"); err != nil {
		t.Fatal(err)
	}
	if got := LoadContext(root); got != "" {
		t.Fatalf("LoadContext picked up the reference: %q", got)
	}
}
