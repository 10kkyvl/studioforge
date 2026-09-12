package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureReferenceIsWriteOnce(t *testing.T) {
	root := t.TempDir()
	if err := EnsureReference(root, ".agent/roblox-ui.md", "shipped"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".agent", "roblox-ui.md")
	if err := os.WriteFile(path, []byte("operator edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureReference(root, ".agent/roblox-ui.md", "new shipped"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "operator edit" {
		t.Fatalf("reference was overwritten: %q", body)
	}
}

func TestEnsureReferenceRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"../outside.md", "/tmp/outside.md", "../../outside.md"} {
		if err := EnsureReference(root, path, "body"); err == nil {
			t.Errorf("EnsureReference(%q) accepted an escaping path", path)
		}
	}
}
