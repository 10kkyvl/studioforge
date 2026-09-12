package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/10kkyvl/studioforge/internal/projects"
)

func TestProjectStylesCanBeListedAndSelected(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	project, err := a.store.Project(t.Context(), "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	list := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/styles")
	if list.Code != 200 {
		t.Fatalf("list styles status=%d body=%s", list.Code, list.Body.String())
	}
	var listed struct {
		Selected string `json:"selected"`
		Styles   []struct {
			Name string `json:"name"`
		} `json:"styles"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Selected != "cute" || len(listed.Styles) < 3 {
		t.Fatalf("unexpected style catalog: %+v", listed)
	}
	selected := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/style", map[string]any{"style": "cyber"})
	if selected.Code != 200 {
		t.Fatalf("select style status=%d body=%s", selected.Code, selected.Body.String())
	}
	if got, err := a.store.ProjectStyle(t.Context(), project.ID); err != nil || got != "cyber" {
		t.Fatalf("stored style=%q err=%v", got, err)
	}
}

func TestProjectStyleSelectionReadsDroppedPackAndPreservesEdits(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	project, err := a.store.Project(t.Context(), "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(project.Path, ".agent", "styles", "farm-custom")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	brief := filepath.Join(dir, "brief.md")
	if err := os.WriteFile(brief, []byte("operator authored"), 0o600); err != nil {
		t.Fatal(err)
	}
	selected := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/style", map[string]any{"style": "farm-custom"})
	if selected.Code != 200 {
		t.Fatalf("select custom style status=%d body=%s", selected.Code, selected.Body.String())
	}
	if err := os.WriteFile(brief, []byte("operator edited after selection"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := projects.EnsureStylePacks(project.Path); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(brief)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "operator edited after selection" {
		t.Fatalf("style pack edit was overwritten: %q", body)
	}
}
