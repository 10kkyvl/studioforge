// Package toolpath locates the external executables StudioForge drives, so an
// operator does not have to hunt for install paths by hand.
package toolpath

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Candidate is one plausible location for a tool.
type Candidate struct {
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source"`
	Status  string `json:"status"` // ok when it answered a version probe, error otherwise
	Message string `json:"message,omitempty"`
}

// Tools are the settings keys this package can fill in.
var Tools = []string{"claude_path", "rojo_path", "git_path", "studio_mcp_path"}

type spec struct {
	command string
	// versionArgs is nil when the tool must not be executed to be identified.
	versionArgs []string
	known       func() []string
}

const probeTimeout = 5 * time.Second

// Detect returns the candidates for one settings key, best first. The result is
// never an error: an absent tool is an ordinary local setup.
func Detect(ctx context.Context, tool string) []Candidate {
	s, ok := specs()[tool]
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []Candidate
	add := func(path, source string) {
		resolved := resolve(path)
		if resolved == "" || seen[strings.ToLower(resolved)] {
			return
		}
		seen[strings.ToLower(resolved)] = true
		out = append(out, probe(ctx, resolved, source, s.versionArgs))
	}
	if s.command != "" {
		if path, err := exec.LookPath(s.command); err == nil {
			add(path, "PATH")
		}
	}
	if s.known != nil {
		for _, path := range s.known() {
			add(path, "known location")
		}
	}
	// A tool that answered a probe outranks one that merely exists on disk.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Status == "ok" && out[j].Status != "ok" })
	return out
}

// DetectAll probes every tool concurrently, since each probe spawns a process.
func DetectAll(ctx context.Context) map[string][]Candidate {
	out := map[string][]Candidate{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, tool := range Tools {
		wg.Add(1)
		go func(tool string) {
			defer wg.Done()
			found := Detect(ctx, tool)
			mu.Lock()
			out[tool] = found
			mu.Unlock()
		}(tool)
	}
	wg.Wait()
	return out
}

func probe(ctx context.Context, path, source string, versionArgs []string) Candidate {
	candidate := Candidate{Path: path, Source: source, Status: "ok"}
	if versionArgs == nil {
		// Identified by existence alone; running it would start a server.
		return candidate
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	body, err := exec.CommandContext(ctx, path, versionArgs...).CombinedOutput()
	if err != nil {
		candidate.Status = "error"
		candidate.Message = strings.TrimSpace(string(body))
		if candidate.Message == "" {
			candidate.Message = err.Error()
		}
		return candidate
	}
	candidate.Version = firstLine(string(body))
	return candidate
}

func firstLine(s string) string {
	if line, _, ok := strings.Cut(strings.TrimSpace(s), "\n"); ok {
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(s)
}

// resolve reports the real path of an existing file, so the same binary reached
// through PATH and through a known location is reported once.
func resolve(path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(real)
	}
	return filepath.Clean(path)
}

func home() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return dir
}

func join(base string, parts ...string) []string {
	if base == "" {
		return nil
	}
	return []string{filepath.Join(append([]string{base}, parts...)...)}
}

func specs() map[string]spec {
	windows := runtime.GOOS == "windows"
	exe := func(name string) string {
		if windows {
			return name + ".exe"
		}
		return name
	}
	return map[string]spec{
		"claude_path": {command: "claude", versionArgs: []string{"--version"}, known: func() []string {
			if windows {
				return concat(
					join(home(), ".local", "bin", "claude.exe"),
					join(os.Getenv("APPDATA"), "npm", "claude.cmd"),
					join(os.Getenv("LOCALAPPDATA"), "Programs", "claude", "claude.exe"),
				)
			}
			return concat(
				join(home(), ".local", "bin", "claude"),
				[]string{"/opt/homebrew/bin/claude", "/usr/local/bin/claude"},
			)
		}},
		"rojo_path": {command: "rojo", versionArgs: []string{"--version"}, known: func() []string {
			paths := concat(
				join(home(), ".cargo", "bin", exe("rojo")),
				join(home(), ".aftman", "bin", exe("rojo")),
				join(home(), ".foreman", "bin", exe("rojo")),
			)
			if !windows {
				paths = append(paths, "/opt/homebrew/bin/rojo", "/usr/local/bin/rojo")
			}
			return paths
		}},
		"git_path": {command: "git", versionArgs: []string{"--version"}, known: func() []string {
			if windows {
				return []string{`C:\Program Files\Git\cmd\git.exe`, `C:\Program Files (x86)\Git\cmd\git.exe`}
			}
			return []string{"/opt/homebrew/bin/git", "/usr/local/bin/git", "/usr/bin/git"}
		}},
		// The launcher starts a server when run, so it is identified by existence.
		"studio_mcp_path": {known: func() []string {
			if windows {
				return join(os.Getenv("LOCALAPPDATA"), "Roblox", "mcp.bat")
			}
			if runtime.GOOS == "darwin" {
				return []string{"/Applications/RobloxStudio.app/Contents/MacOS/StudioMCP"}
			}
			return nil
		}},
	}
}

var ErrToolPathInvalid = errors.New("tool path is invalid")

func IsTool(key string) bool {
	for _, tool := range Tools {
		if tool == key {
			return true
		}
	}
	return false
}

func Validate(tool, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	if hasForbiddenCharacters(value) {
		return "", fmt.Errorf("%w: %q contains a control character, newline, or quote, which cannot appear in a path", ErrToolPathInvalid, value)
	}
	if value != strings.TrimSpace(value) {
		return "", fmt.Errorf("%w: %q has a leading or trailing space", ErrToolPathInvalid, value)
	}
	if !fileExists(value) {
		if first, _, cut := strings.Cut(value, " "); cut && first != "" && (fileExists(first) || lookPathExists(first)) {
			return "", fmt.Errorf("%w: %q looks like a command line with arguments; only the path to the executable itself is accepted, not %q", ErrToolPathInvalid, value, first)
		}
	}
	if containsPathSeparator(value) && !filepath.IsAbs(value) {
		return "", fmt.Errorf("%w: %q contains a path separator but is not an absolute path", ErrToolPathInvalid, value)
	}
	target := value
	if !containsPathSeparator(value) {
		found, err := exec.LookPath(value)
		if err != nil {
			return "", fmt.Errorf("%w: %q was not found on PATH", ErrToolPathInvalid, value)
		}
		target = found
	}
	real := resolve(target)
	if real == "" {
		return "", fmt.Errorf("%w: %q was not found, or is a directory", ErrToolPathInvalid, value)
	}
	if tool == "studio_mcp_path" {
		return real, nil
	}
	if runtime.GOOS == "windows" {
		if !hasExecutableExtension(real) {
			return "", fmt.Errorf("%w: %q does not have an executable extension (expected one of %s)", ErrToolPathInvalid, real, pathext())
		}
		return real, nil
	}
	info, err := os.Stat(real)
	if err != nil || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%w: %q is not executable", ErrToolPathInvalid, real)
	}
	return real, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func lookPathExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func containsPathSeparator(value string) bool {
	if strings.ContainsRune(value, '/') {
		return true
	}
	return runtime.GOOS == "windows" && strings.ContainsRune(value, '\\')
}

func hasForbiddenCharacters(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return strings.ContainsAny(value, `"'`)
}

func pathext() string {
	if value := os.Getenv("PATHEXT"); value != "" {
		return value
	}
	return ".EXE;.COM;.BAT;.CMD"
}

func hasExecutableExtension(path string) bool {
	ext := filepath.Ext(path)
	if ext == "" {
		return false
	}
	for _, candidate := range strings.Split(pathext(), ";") {
		if strings.EqualFold(candidate, ext) {
			return true
		}
	}
	return false
}

func concat(groups ...[]string) []string {
	var out []string
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}
