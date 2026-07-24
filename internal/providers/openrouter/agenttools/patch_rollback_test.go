package agenttools

import (
	"os"
	"path/filepath"
	"testing"
)

// apply_patch promises all-or-nothing. Preparation already fails as a unit,
// but the write pass used to run file by file, so a failure partway through
// left the earlier files changed while the model was told the patch had not
// applied. rollbackEdits is what restores them.
func TestRollbackEditsRestoresAlreadyWrittenFiles(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.lua")
	second := filepath.Join(root, "second.lua")
	if err := os.WriteFile(first, []byte("original first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("original second\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	byPath := map[string]*preparedEdit{
		first:  {resolved: first, content: "patched first\n", original: []byte("original first\n")},
		second: {resolved: second, content: "patched second\n", original: []byte("original second\n")},
	}
	order := []string{first, second}

	// Simulate the write pass getting through the first file and failing on the
	// second, which is exactly what rollbackEdits is handed.
	if err := atomicWriteFile(first, []byte(byPath[first].content)); err != nil {
		t.Fatal(err)
	}
	if name, err := rollbackEdits(byPath, order[:1]); err != nil {
		t.Fatalf("rollback failed on %s: %v", name, err)
	}

	restored, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != "original first\n" {
		t.Fatalf("first file was not rolled back, got %q", restored)
	}
	untouched, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(untouched) != "original second\n" {
		t.Fatalf("second file should never have been written, got %q", untouched)
	}
}
