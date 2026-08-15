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

func TestExportBundleRedactsPathsAndTokensInEveryMember(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	}
	dir := t.TempDir()
	const bearerToken = "BEARERSECRETVALUE1234567890ABCDEF"
	const cookieValue = "COOKIESECRETVALUE1234567890ABCDEF"
	const anthropicKey = "sk-ant-api03-ANTHROPICSECRETVALUE1234567890ABCDEFGHIJ"
	gitOutput := "git version 2.40.0 " +
		"Authorization: Bearer " + bearerToken + " " +
		"Cookie: session=" + cookieValue + " " +
		anthropicKey
	var path, body string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, "git.bat")
		body = "@echo off\r\necho " + gitOutput + "\r\n"
	} else {
		path = filepath.Join(dir, "git")
		body = "#!/bin/sh\necho '" + gitOutput + "'\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	const openrouterSecret = "sk-or-v1-OPENROUTERSECRETVALUE1234567890ABCDEF"
	const nvidiaSecret = "nvapi-NVIDIASECRETVALUE1234567890ABCDEFGHIJ"
	d := &Doctor{
		DataDir:            t.TempDir(),
		OpenRouterKeyState: func(context.Context) string { return openrouterSecret },
		NVIDIAKeyState:     func(context.Context) string { return nvidiaSecret },
	}
	target := filepath.Join(t.TempDir(), "bundle.zip")
	if err := d.ExportBundle(context.Background(), target); err != nil {
		t.Fatal(err)
	}

	r, err := zip.OpenReader(target)
	if err != nil {
		t.Fatalf("zip.OpenReader: %v", err)
	}
	defer r.Close()

	secrets := []string{bearerToken, cookieValue, anthropicKey, openrouterSecret, nvidiaSecret}
	if len(r.File) == 0 {
		t.Fatal("bundle has no members")
	}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		content := string(body)
		for _, secret := range secrets {
			if strings.Contains(content, secret) {
				t.Errorf("member %q leaks a secret: %s", f.Name, content)
			}
		}
	}
}

func TestExportBundleRefusesToLeaveAPartialFileOnFailure(t *testing.T) {
	target := filepath.Join(t.TempDir(), "bundle.zip")

	closedFile, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := closedFile.Close(); err != nil {
		t.Fatal(err)
	}

	orig := createBundleFile
	createBundleFile = func(string) (*os.File, error) { return closedFile, nil }
	defer func() { createBundleFile = orig }()

	d := &Doctor{DataDir: t.TempDir()}
	if err := d.ExportBundle(context.Background(), target); err == nil {
		t.Fatal("expected an error when the bundle file cannot be written")
	}

	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Errorf("expected no partial bundle file left on disk, stat err = %v", statErr)
	}
}
