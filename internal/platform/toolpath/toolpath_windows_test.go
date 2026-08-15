//go:build windows

package toolpath

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func shortPathName(t *testing.T, path string) string {
	t.Helper()
	long, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 4096)
	n, err := windows.GetShortPathName(long, &buf[0], uint32(len(buf)))
	if err != nil {
		t.Skipf("GetShortPathName unavailable: %v", err)
	}
	short := syscall.UTF16ToString(buf[:n])
	if short == path {
		t.Skip("8.3 short names are disabled on this volume")
	}
	return short
}

func TestValidateAcceptsAShortPathFormOnWindows(t *testing.T) {
	dir := t.TempDir()
	real := fakeTool(t, dir, "git", "git version 2.99.0")
	short := shortPathName(t, real)

	normalized, err := Validate("git_path", short)
	if err != nil {
		t.Fatalf("a valid tool reached through its 8.3 short path must be accepted, got err=%v", err)
	}

	info, err := os.Stat(normalized)
	if err != nil {
		t.Fatalf("stat normalized path %q: %v", normalized, err)
	}
	wantInfo, err := os.Stat(real)
	if err != nil {
		t.Fatalf("stat real path %q: %v", real, err)
	}
	if !os.SameFile(info, wantInfo) {
		t.Errorf("normalized=%q, want the same file as %q", normalized, real)
	}
	if normalized != filepath.Clean(normalized) {
		t.Errorf("normalized=%q is not clean", normalized)
	}
}
