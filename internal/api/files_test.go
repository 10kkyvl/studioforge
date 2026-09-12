package api

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/projects"
)

func TestProjectFilesListingAndContent(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	project, err := a.store.Project(t.Context(), "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project.Path, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project.Path, "src", "main.lua"), []byte("print('hello')\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	listing := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/files?path="+url.QueryEscape("src"))
	if listing.Code != 200 {
		t.Fatalf("listing status=%d body=%s", listing.Code, listing.Body.String())
	}
	var listed struct {
		Path    string             `json:"path"`
		Entries []projectFileEntry `json:"entries"`
	}
	if err := json.Unmarshal(listing.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Path != "src" || len(listed.Entries) < 1 {
		t.Fatalf("listing=%+v", listed)
	}
	var mainEntry *projectFileEntry
	for i := range listed.Entries {
		if listed.Entries[i].Name == "main.lua" {
			mainEntry = &listed.Entries[i]
		}
	}
	if mainEntry == nil || mainEntry.Type != "file" || mainEntry.Path != "src/main.lua" {
		t.Fatalf("listing=%+v", listed)
	}

	content := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/files/content?path="+url.QueryEscape("src/main.lua"))
	if content.Code != 200 {
		t.Fatalf("content status=%d body=%s", content.Code, content.Body.String())
	}
	var body projectFileContent
	if err := json.Unmarshal(content.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Content != "print('hello')\n" || body.Binary || body.Size != 15 {
		t.Fatalf("content=%+v", body)
	}
}

func TestProjectFilesRejectPathEscapesAndBinaryOrOversizeContent(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	project, err := a.store.Project(t.Context(), "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(project.Path, "outside.txt")); err != nil {
			t.Fatal(err)
		}
	}
	escapePaths := []string{"../secret.txt", outside}
	if runtime.GOOS != "windows" {
		escapePaths = append(escapePaths, "outside.txt")
	}
	for _, path := range escapePaths {
		rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/files/content?path="+url.QueryEscape(path))
		if rec.Code != 403 {
			t.Fatalf("path %q status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Error.Code != "path_outside_project" {
			t.Fatalf("path %q error=%q", path, envelope.Error.Code)
		}
	}
	if runtime.GOOS != "windows" {
		rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/files?path="+url.QueryEscape("outside.txt"))
		if rec.Code != 403 || !strings.Contains(rec.Body.String(), `"path_outside_project"`) {
			t.Fatalf("symlink listing status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	if err := os.WriteFile(filepath.Join(project.Path, "binary.bin"), []byte{'a', 0, 'b'}, 0o600); err != nil {
		t.Fatal(err)
	}
	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/files/content?path=binary.bin")
	if rec.Code != 200 {
		t.Fatalf("binary status=%d body=%s", rec.Code, rec.Body.String())
	}
	var binary projectFileContent
	if err := json.Unmarshal(rec.Body.Bytes(), &binary); err != nil {
		t.Fatal(err)
	}
	if !binary.Binary || binary.Content != "" {
		t.Fatalf("binary=%+v", binary)
	}
	if err := os.WriteFile(filepath.Join(project.Path, "invalid-utf8.bin"), []byte{0xff, 0xfe, 0xfd}, 0o600); err != nil {
		t.Fatal(err)
	}
	rec = getJSON(t, a, cookie, "/api/v1/projects/demo-obby/files/content?path=invalid-utf8.bin")
	if rec.Code != 200 {
		t.Fatalf("invalid utf8 status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"binary":true`) {
		t.Fatalf("invalid utf8 body=%s", rec.Body.String())
	}

	if err := os.WriteFile(filepath.Join(project.Path, "large.txt"), []byte(strings.Repeat("x", int(maxProjectFileBytes)+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	rec = getJSON(t, a, cookie, "/api/v1/projects/demo-obby/files/content?path=large.txt")
	if rec.Code != 413 || !strings.Contains(rec.Body.String(), `"file_too_large"`) {
		t.Fatalf("large status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProjectFilesLazyRegistersProjectRoot(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	project, err := a.store.Project(t.Context(), "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a project root that was unavailable during daemon startup by
	// using a fresh guard with no registrations, while keeping the project row.
	a.server.guard = projects.NewPathGuard()
	if err := os.WriteFile(filepath.Join(project.Path, "lazy.txt"), []byte("available"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/files/content?path=lazy.txt")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "available") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
