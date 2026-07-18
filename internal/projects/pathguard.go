package projects

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var ErrOutsideProject = errors.New("path is outside the registered project root")

type PathGuard struct {
	mu    sync.RWMutex
	roots map[string]string
}

func NewPathGuard() *PathGuard { return &PathGuard{roots: map[string]string{}} }

func Canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("absolute path: %w", err)
	}
	abs = filepath.Clean(abs)
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}
	parent := filepath.Dir(abs)
	resolvedParent, perr := filepath.EvalSymlinks(parent)
	if perr != nil {
		return "", fmt.Errorf("resolve parent symlinks: %w", perr)
	}
	return filepath.Join(resolvedParent, filepath.Base(abs)), nil
}

func (g *PathGuard) Register(id, path string) (string, error) {
	root, err := Canonical(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("inspect project root: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("project root is not a directory")
	}
	g.mu.Lock()
	g.roots[id] = root
	g.mu.Unlock()
	return root, nil
}

func (g *PathGuard) Resolve(projectID, relative string) (string, error) {
	g.mu.RLock()
	root, ok := g.roots[projectID]
	g.mu.RUnlock()
	if !ok {
		return "", errors.New("project root is not registered")
	}
	if filepath.IsAbs(relative) {
		return "", ErrOutsideProject
	}
	target, err := Canonical(filepath.Join(root, relative))
	if err != nil {
		return "", err
	}
	if !within(root, target) {
		return "", ErrOutsideProject
	}
	return target, nil
}

func within(root, target string) bool {
	r := filepath.Clean(root)
	t := filepath.Clean(target)
	if runtime.GOOS == "windows" {
		r = strings.ToLower(r)
		t = strings.ToLower(t)
	}
	rel, err := filepath.Rel(r, t)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func Fingerprint(path string) string {
	sum := sha256.Sum256([]byte(normalize(path)))
	return hex.EncodeToString(sum[:])
}
func normalize(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}
