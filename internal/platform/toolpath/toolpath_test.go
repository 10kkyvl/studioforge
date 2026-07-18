package toolpath

import (
	"context"
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
	if found := Detect(context.Background(), "codex_path"); len(found) != 0 {
		t.Errorf("expected no candidates, got %+v", found)
	}
}

// A binary that is both on PATH and at a known install location is one binary,
// and offering it twice would make the settings UI look broken.
func TestDetectDeduplicatesTheSameBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// Keep the machine's real Codex out of this probe.
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())

	// Write it at the exact path the known-location list expects, then put that
	// same directory on PATH so both discovery routes reach it.
	local := filepath.Join(home, ".local", "bin")
	name := "codex"
	if runtime.GOOS == "windows" {
		name = "codex.exe"
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(local, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho codex 1.2.3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", local)

	found := Detect(context.Background(), "codex_path")
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
