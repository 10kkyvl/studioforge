package toolpath

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PrepareGUIEnvironment makes conventionally installed tools available to a
// Finder-launched app and its children. Preserve the operator's PATH order;
// never execute login-shell configuration merely to discover executables.
func PrepareGUIEnvironment() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return os.Setenv("PATH", appendToolDirectories(os.Getenv("PATH"), []string{
		filepath.Join(homeDir, ".local", "bin"),
		"/opt/homebrew/bin", "/usr/local/bin",
		filepath.Join(homeDir, ".cargo", "bin"),
		filepath.Join(homeDir, ".aftman", "bin"),
		filepath.Join(homeDir, ".foreman", "bin"),
	}))
}

func appendToolDirectories(current string, candidates []string) string {
	paths := filepath.SplitList(current)
	seen := map[string]bool{}
	for _, path := range paths {
		seen[filepath.Clean(path)] = true
	}
	for _, path := range candidates {
		if seen[filepath.Clean(path)] {
			continue
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			continue
		}
		paths = append(paths, path)
		seen[filepath.Clean(path)] = true
	}
	return strings.Join(paths, string(os.PathListSeparator))
}
