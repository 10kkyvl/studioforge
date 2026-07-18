package projects

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPathGuardRejectsTraversalAndAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := NewPathGuard()
	if _, err := g.Register("p", root); err != nil {
		t.Fatal(err)
	}
	path, err := g.Resolve("p", "ok.txt")
	if err != nil || filepath.Base(path) != "ok.txt" {
		t.Fatalf("path=%s err=%v", path, err)
	}
	for _, bad := range []string{"../outside.txt", filepath.Join(filepath.VolumeName(root)+string(filepath.Separator), "outside.txt")} {
		if _, err := g.Resolve("p", bad); !errors.Is(err, ErrOutsideProject) {
			t.Errorf("%q error=%v", bad, err)
		}
	}
}
func TestPathGuardRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	g := NewPathGuard()
	if _, err := g.Register("p", root); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Resolve("p", filepath.Join("escape", "value.txt")); !errors.Is(err, ErrOutsideProject) {
		t.Fatalf("error=%v", err)
	}
}
