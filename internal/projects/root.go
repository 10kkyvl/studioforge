package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var ErrUnsafeProjectRoot = errors.New("project location is not allowed")
var ErrProjectParentMissing = errors.New("parent directory of project location does not exist")

func PrepareRoot(path, dataDir string, create bool) (string, error) {
	canonical, err := Canonical(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %s", ErrProjectParentMissing, path)
		}
		return "", err
	}
	if err := checkSafeRoot(canonical, dataDir); err != nil {
		return "", err
	}
	info, statErr := os.Stat(canonical)
	if statErr == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("%w: %s is not a directory", ErrUnsafeProjectRoot, canonical)
		}
		return canonical, nil
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("inspect project root: %w", statErr)
	}
	if !create {
		return "", fmt.Errorf("inspect project root: %w", statErr)
	}
	parent := filepath.Dir(canonical)
	parentInfo, err := os.Stat(parent)
	if err != nil || !parentInfo.IsDir() {
		return "", fmt.Errorf("%w: %s", ErrProjectParentMissing, parent)
	}
	if err := os.Mkdir(canonical, 0o700); err != nil {
		return "", fmt.Errorf("create project directory: %w", err)
	}
	return canonical, nil
}

func checkSafeRoot(canonical, dataDir string) error {
	if filepath.Dir(canonical) == canonical {
		return fmt.Errorf("%w: %s is a volume root", ErrUnsafeProjectRoot, canonical)
	}
	for _, forbidden := range denylistedRoots() {
		if forbidden == "" {
			continue
		}
		if pathWithinOrEqual(canonical, forbidden) {
			return fmt.Errorf("%w: %s is a system directory", ErrUnsafeProjectRoot, canonical)
		}
	}
	if dataDir == "" {
		return nil
	}
	canonicalData, err := Canonical(dataDir)
	if err != nil {
		canonicalData = filepath.Clean(dataDir)
	}
	if pathWithinOrEqual(canonical, canonicalData) {
		return fmt.Errorf("%w: %s is inside the StudioForge data directory", ErrUnsafeProjectRoot, canonical)
	}
	return nil
}

func denylistedRoots() []string {
	if runtime.GOOS == "windows" {
		systemRoot := os.Getenv("SystemRoot")
		if systemRoot == "" {
			systemRoot = `C:\Windows`
		}
		programFiles := os.Getenv("ProgramFiles")
		if programFiles == "" {
			programFiles = `C:\Program Files`
		}
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		if programFilesX86 == "" {
			programFilesX86 = `C:\Program Files (x86)`
		}
		return []string{systemRoot, programFiles, programFilesX86}
	}
	return []string{"/System", "/usr", "/bin", "/sbin", "/etc", "/Library", "/private"}
}

func pathWithinOrEqual(path, dir string) bool {
	p, d := path, dir
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
		d = strings.ToLower(d)
	}
	if p == d {
		return true
	}
	return strings.HasPrefix(p, d+string(filepath.Separator))
}
