package diagnostics

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/10kkyvl/studioforge/internal/database"
)

func fakeGit(t *testing.T, dir, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var path, body string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, "git.bat")
		body = "@echo off\r\necho " + version + "\r\n"
	} else {
		path = filepath.Join(dir, "git")
		body = "#!/bin/sh\necho '" + version + "'\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestRunDependencyGitPresentOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
	dir := t.TempDir()
	fakeGit(t, dir, "git version 2.99.0")
	t.Setenv("PATH", dir)

	d := &Doctor{DataDir: t.TempDir()}
	report := d.Run(context.Background())

	got, ok := report.Dependencies["git"]
	if !ok {
		t.Fatal("expected a git dependency entry")
	}
	if got.Status != "ok" {
		t.Errorf("status = %q, want ok (message=%q)", got.Status, got.Message)
	}
	if got.Version != "git version 2.99.0" {
		t.Errorf("version = %q", got.Version)
	}
}

func TestRunDependencyGitAbsentFromPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	d := &Doctor{DataDir: t.TempDir()}
	report := d.Run(context.Background())

	got, ok := report.Dependencies["git"]
	if !ok {
		t.Fatal("expected a git dependency entry")
	}
	if got.Status != "missing" {
		t.Errorf("status = %q, want missing", got.Status)
	}
}

func TestRunStudioMcpOverridePresent(t *testing.T) {
	override := filepath.Join(t.TempDir(), "mcp-launcher")
	if err := os.WriteFile(override, []byte("launcher"), 0o755); err != nil {
		t.Fatal(err)
	}

	d := &Doctor{DataDir: t.TempDir(), MCPOverride: override}
	report := d.Run(context.Background())

	got, ok := report.Dependencies["studioMcp"]
	if !ok {
		t.Fatal("expected a studioMcp dependency entry")
	}
	if got.Status != "ok" {
		t.Errorf("status = %q, want ok (message=%q)", got.Status, got.Message)
	}
}

func TestRunStudioMcpOverrideAbsent(t *testing.T) {
	d := &Doctor{DataDir: t.TempDir(), MCPOverride: filepath.Join(t.TempDir(), "does-not-exist")}
	report := d.Run(context.Background())

	got, ok := report.Dependencies["studioMcp"]
	if !ok {
		t.Fatal("expected a studioMcp dependency entry")
	}
	if got.Status != "missing" {
		t.Errorf("status = %q, want missing", got.Status)
	}
}

func TestRunSkipsOptionalProvidersWhenNil(t *testing.T) {
	d := &Doctor{DataDir: t.TempDir()}
	report := d.Run(context.Background())

	for _, key := range []string{"claude", "rojo"} {
		if _, ok := report.Dependencies[key]; ok {
			t.Errorf("dependency %q must be absent when its provider is nil", key)
		}
	}
}

func TestRunWritableDataDirectoryReportsOk(t *testing.T) {
	d := &Doctor{DataDir: t.TempDir()}
	report := d.Run(context.Background())

	var found bool
	for _, check := range report.Checks {
		if check.Name != "dataDirectory" {
			continue
		}
		found = true
		if check.Status != "ok" {
			t.Errorf("status = %q, want ok (message=%q)", check.Status, check.Message)
		}
	}
	if !found {
		t.Fatal("expected a dataDirectory check")
	}
}

func TestRunDependencyNvidiaAbsentWhenKeyStateNil(t *testing.T) {
	d := &Doctor{DataDir: t.TempDir()}
	report := d.Run(context.Background())

	if _, ok := report.Dependencies["nvidia"]; ok {
		t.Error("expected no nvidia dependency entry when NVIDIAKeyState is nil")
	}
}

func TestRunDependencyNvidiaKeyStateStatus(t *testing.T) {
	cases := []struct {
		name     string
		keyState string
		want     string
	}{
		{"configured", "configured", "ok"},
		{"unverified", "unverified", "warning"},
		{"invalid", "invalid", "error"},
		{"empty", "", "missing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &Doctor{DataDir: t.TempDir(), NVIDIAKeyState: func(context.Context) string { return c.keyState }}
			report := d.Run(context.Background())

			got, ok := report.Dependencies["nvidia"]
			if !ok {
				t.Fatal("expected an nvidia dependency entry")
			}
			if got.Status != c.want {
				t.Errorf("status = %q, want %q (message=%q)", got.Status, c.want, got.Message)
			}
		})
	}
}

func TestRunDatabaseIntegrityOk(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "doctor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	d := &Doctor{DB: db, DataDir: t.TempDir()}
	report := d.Run(ctx)

	if report.Database != "ok" {
		t.Errorf("database = %q, want ok", report.Database)
	}
	var found bool
	for _, check := range report.Checks {
		if check.Name == "database" {
			found = true
			if check.Status != "ok" {
				t.Errorf("status = %q, want ok (message=%q)", check.Status, check.Message)
			}
		}
	}
	if !found {
		t.Fatal("expected a database check")
	}
}
