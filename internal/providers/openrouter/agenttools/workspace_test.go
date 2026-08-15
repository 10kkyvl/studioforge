package agenttools

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathWithinRootFoldsCaseWhenTheFilesystemDoes(t *testing.T) {
	root := t.TempDir()
	if !caseInsensitiveRoot(root) {
		t.Skip("temp dir is on a case-sensitive filesystem; nothing to fold")
	}
	differentlyCasedRoot := swapCase(root)
	if differentlyCasedRoot == root {
		t.Skip("temp dir path has no letters to swap the case of")
	}
	target := filepath.Join(differentlyCasedRoot, "child.txt")
	if !pathWithinRoot(root, target, true) {
		t.Errorf("pathWithinRoot(%q, %q, true) = false, want true on a case-insensitive filesystem", root, target)
	}
	if pathWithinRoot(root, target, false) {
		t.Errorf("pathWithinRoot(%q, %q, false) = true, want it to compare literally when told not to fold", root, target)
	}
}

func TestCaseInsensitiveRootDetectsTheFilesystem(t *testing.T) {
	root := t.TempDir()
	got := caseInsensitiveRoot(root)
	if runtime.GOOS == "windows" && !got {
		t.Errorf("caseInsensitiveRoot(%q) = false on Windows, want true", root)
	}
	if got != caseInsensitiveRoot(root) {
		t.Errorf("caseInsensitiveRoot(%q) is not stable across repeated calls", root)
	}
}

func TestCaseInsensitiveRootFallsBackWhenTheProbeIsUninformative(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	got := caseInsensitiveRoot(missing)
	want := runtime.GOOS == "windows" || runtime.GOOS == "darwin"
	if got != want {
		t.Errorf("caseInsensitiveRoot(%q) = %v, want %v (an uninformative probe should fall back by platform)", missing, got, want)
	}
}

func TestSwapCaseLeavesACaseInvariantStringUnchanged(t *testing.T) {
	invariant := `0123456789-_.\/:`
	if got := swapCase(invariant); got != invariant {
		t.Errorf("swapCase(%q) = %q, want it unchanged since it has no letters", invariant, got)
	}
	if got := swapCase("MixedCase"); got == "MixedCase" {
		t.Errorf("swapCase(%q) left the string unchanged, want the case of every letter flipped", "MixedCase")
	}
}
