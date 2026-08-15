package toolpath

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeTool writes an executable that reports a version, so the probe has
// something real to run without depending on what is installed.
func fakeTool(t *testing.T, dir, name, version string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var path, body string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, name+".bat")
		body = "@echo off\r\necho " + version + "\r\n"
	} else {
		path = filepath.Join(dir, name)
		body = "#!/bin/sh\necho '" + version + "'\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDetectFindsToolOnPathAndReadsVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		// exec.LookPath honours PATHEXT; .bat is covered, but keep the probe honest
		// by checking the shim actually runs first.
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
	dir := t.TempDir()
	fakeTool(t, dir, "git", "git version 2.99.0")
	t.Setenv("PATH", dir)

	found := Detect(context.Background(), "git_path")
	if len(found) == 0 {
		t.Fatal("a tool on PATH must be detected")
	}
	if found[0].Source != "PATH" {
		t.Errorf("source=%q want PATH", found[0].Source)
	}
	if found[0].Status != "ok" {
		t.Errorf("status=%q message=%q", found[0].Status, found[0].Message)
	}
	if found[0].Version != "git version 2.99.0" {
		t.Errorf("version=%q", found[0].Version)
	}
}

func TestDetectReportsNothingWhenAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	// git_path also probes absolute system locations, so use a tool whose known
	// locations are all under the redirected home.
	if found := Detect(context.Background(), "claude_path"); len(found) != 0 {
		t.Errorf("expected no candidates, got %+v", found)
	}
}

// A binary that is both on PATH and at a known install location is one binary,
// and offering it twice would make the settings UI look broken.
func TestDetectDeduplicatesTheSameBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// Keep the machine's real Claude out of this probe.
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())

	// Write it at the exact path the known-location list expects, then put that
	// same directory on PATH so both discovery routes reach it.
	local := filepath.Join(home, ".local", "bin")
	name := "claude"
	if runtime.GOOS == "windows" {
		name = "claude.exe"
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(local, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho claude 1.2.3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", local)

	found := Detect(context.Background(), "claude_path")
	if len(found) != 1 {
		t.Fatalf("the same binary must be reported once, got %+v", found)
	}
	if found[0].Source != "PATH" {
		t.Errorf("PATH should win the dedup, got source=%q", found[0].Source)
	}
}

func TestDetectMarksBrokenBinaries(t *testing.T) {
	dir := t.TempDir()
	var path string
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
		path = filepath.Join(dir, "git.bat")
		if err := os.WriteFile(path, []byte("@echo off\r\nexit /b 3\r\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		path = filepath.Join(dir, "git")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	found := Detect(context.Background(), "git_path")
	if len(found) == 0 {
		t.Fatal("a present but broken tool must still be reported")
	}
	var sawError bool
	for _, candidate := range found {
		if candidate.Path == path || candidate.Status == "error" {
			sawError = candidate.Status == "error"
			break
		}
	}
	if !sawError {
		t.Errorf("a tool that fails its version probe must be status=error, got %+v", found)
	}
}

func TestDetectUnknownToolIsEmpty(t *testing.T) {
	if found := Detect(context.Background(), "not_a_tool"); found != nil {
		t.Errorf("unknown key must yield nothing, got %+v", found)
	}
}

func TestDetectAllCoversEveryTool(t *testing.T) {
	all := DetectAll(context.Background())
	for _, tool := range Tools {
		if _, ok := all[tool]; !ok {
			t.Errorf("DetectAll omitted %q", tool)
		}
	}
}

// The launcher starts a proxy server when executed, so detection must not run it.
func TestStudioLauncherIsNotExecuted(t *testing.T) {
	if s, ok := specs()["studio_mcp_path"]; !ok || s.versionArgs != nil {
		t.Error("studio_mcp_path must be identified by existence, never by execution")
	}
}

func TestValidateAcceptsEmptyAsAutoDetect(t *testing.T) {
	for _, value := range []string{"", "   "} {
		normalized, err := Validate("git_path", value)
		if err != nil || normalized != "" {
			t.Errorf("value=%q normalized=%q err=%v, want (\"\", nil)", value, normalized, err)
		}
	}
}

func TestValidateRefusesADirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := Validate("git_path", dir); !errors.Is(err, ErrToolPathInvalid) {
		t.Fatalf("a directory must be refused, got err=%v", err)
	}
}

func TestValidateRefusesAMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.exe")
	if _, err := Validate("claude_path", missing); !errors.Is(err, ErrToolPathInvalid) {
		t.Fatalf("a missing file must be refused, got err=%v", err)
	}
}

func TestValidateRefusesArgumentsInsideThePath(t *testing.T) {
	dir := t.TempDir()
	path := fakeTool(t, dir, "git", "git version 2.99.0")
	if _, err := Validate("git_path", path+" --upload-pack=evil"); !errors.Is(err, ErrToolPathInvalid) {
		t.Fatalf("trailing arguments must be refused, got err=%v", err)
	}
}

func TestValidateRefusesNewlinesAndQuotes(t *testing.T) {
	dir := t.TempDir()
	path := fakeTool(t, dir, "git", "git version 2.99.0")
	for _, value := range []string{path + "\n", path + "\r", `"` + path + `"`, path + "'"} {
		if _, err := Validate("git_path", value); !errors.Is(err, ErrToolPathInvalid) {
			t.Errorf("value=%q must be refused, got err=%v", value, err)
		}
	}
}

func TestValidateRefusesASurroundingSpace(t *testing.T) {
	dir := t.TempDir()
	path := fakeTool(t, dir, "git", "git version 2.99.0")
	for _, value := range []string{" " + path, path + " "} {
		if _, err := Validate("git_path", value); !errors.Is(err, ErrToolPathInvalid) {
			t.Errorf("value=%q must be refused, got err=%v", value, err)
		}
	}
}

func TestValidateRefusesANonExecutableExtensionOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PATHEXT enforcement only applies on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.txt")
	if err := os.WriteFile(path, []byte("not an executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate("claude_path", path); !errors.Is(err, ErrToolPathInvalid) {
		t.Fatalf("a non-PATHEXT extension must be refused, got err=%v", err)
	}
}

func TestValidateResolvesSymlinkToRealPath(t *testing.T) {
	dir := t.TempDir()
	real := fakeTool(t, dir, "git", "git version 2.99.0")
	link := filepath.Join(dir, "git-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable in this environment: %v", err)
	}
	normalized, err := Validate("git_path", link)
	if err != nil {
		t.Fatalf("a symlink to a valid tool must be accepted, got err=%v", err)
	}
	want := resolve(real)
	if normalized != want {
		t.Errorf("normalized=%q want=%q (the real path, not the symlink)", normalized, want)
	}
}

func TestValidateAllowsStudioMCPWithoutAnExecuteBit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.launcher")
	if err := os.WriteFile(path, []byte("launcher"), 0o644); err != nil {
		t.Fatal(err)
	}
	normalized, err := Validate("studio_mcp_path", path)
	if err != nil {
		t.Fatalf("studio_mcp_path must not require an execute bit, got err=%v", err)
	}
	if normalized == "" {
		t.Error("normalized path must not be empty")
	}
}

func TestValidateAllowsABareNameFoundOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
	dir := t.TempDir()
	fakeTool(t, dir, "git", "git version 2.99.0")
	t.Setenv("PATH", dir)
	normalized, err := Validate("git_path", "git")
	if err != nil {
		t.Fatalf("a bare name found on PATH must be accepted, got err=%v", err)
	}
	if normalized == "" {
		t.Error("normalized path must not be empty")
	}
}
