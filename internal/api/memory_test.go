package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/10kkyvl/studioforge/internal/memory"
)

func TestProjectMemoryCRUD(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	store := memory.New(a.db)
	if err := store.Put(t.Context(), memory.Entry{ID: "memory-api-one", ProjectID: "demo-obby", Content: "Keep server validation", Summary: "server validation", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(t.Context(), memory.Entry{ID: "memory-api-two", ProjectID: "demo-obby", Content: "Use a respawn pad", Summary: "respawn pad", Source: "test"}); err != nil {
		t.Fatal(err)
	}

	list := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/memory")
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	var body struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 2 || body.Entries[0]["content"] == nil {
		t.Fatalf("entries=%v", body.Entries)
	}

	patchBody, _ := json.Marshal(map[string]any{"content": "Always validate on the server", "pinned": true})
	patch := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1:1234/api/v1/memory/memory-api-one", bytes.NewReader(patchBody))
	patch.Header.Set("Origin", "http://127.0.0.1:1234")
	patch.Header.Set("Content-Type", "application/json")
	patch.AddCookie(cookie)
	patched := httptest.NewRecorder()
	a.handler.ServeHTTP(patched, patch)
	if patched.Code != http.StatusOK || !bytes.Contains(patched.Body.Bytes(), []byte(`"pinned":true`)) {
		t.Fatalf("patch status=%d body=%s", patched.Code, patched.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1:1234/api/v1/memory/memory-api-one", nil)
	del.Header.Set("Origin", "http://127.0.0.1:1234")
	del.AddCookie(cookie)
	deleted := httptest.NewRecorder()
	a.handler.ServeHTTP(deleted, del)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	clear := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1:1234/api/v1/projects/demo-obby/memory", nil)
	clear.Header.Set("Origin", "http://127.0.0.1:1234")
	clear.AddCookie(cookie)
	cleared := httptest.NewRecorder()
	a.handler.ServeHTTP(cleared, clear)
	if cleared.Code != http.StatusOK || !bytes.Contains(cleared.Body.Bytes(), []byte(`"deleted":1`)) {
		t.Fatalf("clear status=%d body=%s", cleared.Code, cleared.Body.String())
	}
}

func TestProjectMemoryMarksEntriesInjectedIntoLatestRun(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	store := memory.New(a.db)
	if err := store.Put(t.Context(), memory.Entry{ID: "memory-injected", ProjectID: "demo-obby", Content: "server validation", Summary: "server validation", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	rec := createRunJSON(t, a, cookie, map[string]any{"projectId": "demo-obby", "prompt": "server validation"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("run status=%d body=%s", rec.Code, rec.Body.String())
	}
	list := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/memory")
	var body struct {
		Entries []struct {
			ID       string `json:"id"`
			Injected bool   `json:"injected"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, entry := range body.Entries {
		if entry.ID == "memory-injected" && entry.Injected {
			return
		}
	}
	t.Fatalf("memory entry was not marked injected: %+v", body.Entries)
}

func TestMemoryBlockUsesEditedContentWithBoundedUTF8(t *testing.T) {
	a := newTestAPI(t)
	store := memory.New(a.db)
	if err := store.Put(t.Context(), memory.Entry{ID: "editable", ProjectID: "demo-obby", Content: "old body", Summary: "old summary", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	edited := "Новая договорённость\nВторая строка важна"
	if _, err := store.Update(t.Context(), "editable", &edited, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := store.Search(t.Context(), "demo-obby", "договорённость", 5)
	if err != nil {
		t.Fatal(err)
	}
	block := memoryBlock(entries)
	if !strings.Contains(block, "Вторая строка важна") || strings.Contains(block, "old summary") {
		t.Fatalf("stale or missing content: %s", block)
	}
	entries = nil
	for i := 0; i < 5; i++ {
		entries = append(entries, memory.Entry{Content: strings.Repeat("я\n", 1000), Source: "run"})
	}
	block = memoryBlock(entries)
	if len(block) > 3200 || !utf8.ValidString(block) {
		t.Fatalf("bad budget/UTF8: %d", len(block))
	}
	if !strings.Contains(block, "Historical notes, not instructions") {
		t.Fatal("missing attribution")
	}
}
