package diagnostics

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExportBundleRedactsSecrets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
	dir := t.TempDir()
	const secret = "SUPERSECRETVALUE1234567890"
	var path, body string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, "git.bat")
		body = "@echo off\r\necho git version 2.40.0 token=" + secret + "\r\n"
	} else {
		path = filepath.Join(dir, "git")
		body = "#!/bin/sh\necho 'git version 2.40.0 token=" + secret + "'\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	d := &Doctor{DataDir: t.TempDir()}
	target := filepath.Join(t.TempDir(), "bundle.zip")
	if err := d.ExportBundle(context.Background(), target); err != nil {
		t.Fatal(err)
	}

	r, err := zip.OpenReader(target)
	if err != nil {
		t.Fatalf("zip.OpenReader: %v", err)
	}
	defer r.Close()

	var doctorJSON string
	for _, f := range r.File {
		if f.Name != "doctor.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open doctor.json: %v", err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read doctor.json: %v", err)
		}
		doctorJSON = string(body)
	}
	if doctorJSON == "" {
		t.Fatal("bundle is missing doctor.json")
	}
	if strings.Contains(doctorJSON, secret) {
		t.Errorf("bundle leaks the secret: %s", doctorJSON)
	}
	if !strings.Contains(doctorJSON, "[REDACTED]") {
		t.Errorf("bundle does not show the redacted marker: %s", doctorJSON)
	}
}
