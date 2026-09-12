package toolpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGUIPathPreservesOverridesAndOnlyAddsExistingDirectories(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	got := appendToolDirectories(first, []string{first, second, second, file, filepath.Join(second, "absent")})
	want := strings.Join([]string{first, second}, string(os.PathListSeparator))
	if got != want {
		t.Fatalf("PATH=%q want %q", got, want)
	}
}
