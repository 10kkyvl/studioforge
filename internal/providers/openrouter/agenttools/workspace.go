package agenttools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Workspace struct {
	root string
}

func NewWorkspace(root string) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("agenttools: resolve workspace root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("agenttools: workspace root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("agenttools: workspace root %s is not a directory", abs)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("agenttools: resolve workspace root symlinks: %w", err)
	}
	return &Workspace{root: real}, nil
}

func (w *Workspace) Root() string { return w.root }

func (w *Workspace) Resolve(rel string) (string, error) {
	if rel == "" {
		rel = "."
	}
	if looksAbsolute(rel) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", rel)
	}
	cleaned := filepath.Clean(rel)
	joined := filepath.Join(w.root, cleaned)
	if !pathWithinRoot(w.root, joined) {
		return "", fmt.Errorf("path escapes workspace root: %s", rel)
	}
	if err := ensureNoSymlinkEscape(w.root, joined); err != nil {
		return "", err
	}
	return joined, nil
}

func looksAbsolute(p string) bool {
	if p == "" {
		return false
	}
	if filepath.IsAbs(p) {
		return true
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
		return true
	}
	if len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0]) {
		return true
	}
	return false
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func pathWithinRoot(root, target string) bool {
	r, t := root, target
	if runtime.GOOS == "windows" {
		r = strings.ToLower(r)
		t = strings.ToLower(t)
	}
	if r == t {
		return true
	}
	return strings.HasPrefix(t, r+string(filepath.Separator))
}

func ensureNoSymlinkEscape(root, target string) error {
	path := target
	for {
		real, err := filepath.EvalSymlinks(path)
		if err == nil {
			if !pathWithinRoot(root, real) {
				return errors.New("path resolves outside workspace root via a symlink")
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("resolve path: %w", err)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return errors.New("cannot resolve path")
		}
		path = parent
	}
}
