package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalResolvesAParentSymlinkForANonExistentLeaf(t *testing.T) {
	real := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, err := Canonical(filepath.Join(link, "missing-leaf.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	want = filepath.Join(want, "missing-leaf.txt")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPathGuardRegisterCanonicalizesTheRoot(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	uncleaned := filepath.Join(nested, "..") + string(filepath.Separator)

	g := NewPathGuard()
	got, err := g.Register("p", uncleaned)
	if err != nil {
		t.Fatal(err)
	}

	want, err := Canonical(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.Contains(got, "..") {
		t.Fatalf("Register did not canonicalize the path: %q", got)
	}
}
