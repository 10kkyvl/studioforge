package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

// tinyPNG is a real, minimal 1x1 transparent PNG — small enough to embed
// here, but genuine enough that http.DetectContentType sniffs it as
// image/png the same way an actual pasted screenshot would, exercising the
// real MIME allowlist rather than a stub.
var tinyPNG = mustBase64Decode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")

func mustBase64Decode(s string) []byte {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return data
}

func postAttachment(t *testing.T, a *testAPI, cookie *http.Cookie, projectID, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/projects/"+projectID+"/attachments", &buf)
	req.Header.Set("Origin", "http://127.0.0.1:1234")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

func TestUploadAttachmentStoresAndDedupes(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := postAttachment(t, a, cookie, "demo-obby", "shot.png", tinyPNG)
	if rec.Code != 201 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body.Path, ".studioforge/attachments/") || !strings.HasSuffix(body.Path, ".png") {
		t.Fatalf("path=%q, want .studioforge/attachments/<date>-<hash>.png", body.Path)
	}
	project, err := a.store.Project(context.Background(), "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(project.Path, filepath.FromSlash(body.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, tinyPNG) {
		t.Error("stored bytes must match the upload")
	}

	// Pasting the same image again — even under a different original filename —
	// must resolve to the same content-addressed path rather than writing a
	// second copy.
	rec2 := postAttachment(t, a, cookie, "demo-obby", "shot-again.png", tinyPNG)
	if rec2.Code != 201 {
		t.Fatalf("status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var body2 struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &body2); err != nil {
		t.Fatal(err)
	}
	if body2.Path != body.Path {
		t.Errorf("dedup path=%q want %q", body2.Path, body.Path)
	}
	entries, err := os.ReadDir(filepath.Join(project.Path, ".studioforge", "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("attachments dir has %d entries, want exactly 1 after a duplicate paste", len(entries))
	}
}

func TestUploadAttachmentRejectsUnsupportedMIME(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := postAttachment(t, a, cookie, "demo-obby", "notes.png", []byte("just plain text, not an image, despite the .png name"))
	if rec.Code != 400 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUploadAttachmentRejectsOversizedFile(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	// A real PNG signature followed by enough padding to clear the 10 MB cap.
	// http.DetectContentType only sniffs the first 512 bytes, so this still
	// reads as image/png — the test isolates the size check from the MIME
	// check, rather than getting rejected for the wrong reason.
	oversized := append([]byte{}, tinyPNG...)
	oversized = append(oversized, make([]byte, maxAttachmentBytes)...)
	rec := postAttachment(t, a, cookie, "demo-obby", "huge.png", oversized)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUploadAttachmentUnknownProject(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := postAttachment(t, a, cookie, "nope", "shot.png", tinyPNG)
	if rec.Code != 404 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUploadAttachmentRequiresFileField(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("caption", "no file here")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/projects/demo-obby/attachments", &buf)
	req.Header.Set("Origin", "http://127.0.0.1:1234")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetAttachmentServesStoredFile(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	uploadRec := postAttachment(t, a, cookie, "demo-obby", "shot.png", tinyPNG)
	var uploaded struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/attachments/"+filepath.Base(uploaded.Path))
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("content-type=%q want image/png", rec.Header().Get("Content-Type"))
	}
	if !bytes.Equal(rec.Body.Bytes(), tinyPNG) {
		t.Error("served bytes must match the upload")
	}
}

func TestGetAttachmentMissingFile(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/attachments/2026-07-19-deadbeef0000.png")
	if rec.Code != 404 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetAttachmentUnknownProject(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := getJSON(t, a, cookie, "/api/v1/projects/nope/attachments/2026-07-19-deadbeef0000.png")
	if rec.Code != 404 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// Exercises the handler directly with {name} values a well-behaved router
// would never hand it, the same way a hostile client could try to smuggle
// one past URL cleaning/decoding quirks. SetPathValue bypasses the mux
// entirely so this proves the handler defends itself, not that the mux
// happens to.
func TestGetAttachmentHandlerRejectsTraversalPathValues(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	malicious := []string{"..", "../secret.txt", "../../secret.txt", "a/../../secret.txt", "..\\secret.txt", ".", ""}
	for _, name := range malicious {
		req := httptest.NewRequest("GET", "http://127.0.0.1:1234/api/v1/projects/demo-obby/attachments/x", nil)
		req.SetPathValue("id", "demo-obby")
		req.SetPathValue("name", name)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		a.server.getAttachment(rec, req)
		if rec.Code == 200 {
			t.Errorf("name=%q must not be served, status=%d body=%s", name, rec.Code, rec.Body.String())
		}
	}
}

// A companion secret file just outside the attachments directory proves
// resolveAttachment refuses to reach it, rather than merely refusing names
// that happen to look suspicious.
func TestResolveAttachmentRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, chatAttachmentsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("do not serve me"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"..", "../secret.txt", "../../secret.txt", "a/../../secret.txt", "..\\secret.txt", ".", "", "/etc/passwd"} {
		if resolved, err := resolveAttachment(root, name); err == nil {
			t.Errorf("resolveAttachment(%q) = %q, want an error", name, resolved)
		}
	}
	// A legitimate, single-segment name must still resolve, so the rejection
	// above is proven to be about traversal specifically, not everything.
	legit, err := resolveAttachment(root, "2026-07-19-abc123def456.png")
	if err != nil {
		t.Fatalf("a plain filename must resolve: %v", err)
	}
	wantSuffix := filepath.Join(chatAttachmentsDir, "2026-07-19-abc123def456.png")
	if !strings.HasSuffix(legit, wantSuffix) {
		t.Errorf("resolved=%q, want a suffix of %q", legit, wantSuffix)
	}
}

func TestCreateRunAppendsAttachmentsBlockToPrompt(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	uploadRec := postAttachment(t, a, cookie, "demo-obby", "shot.png", tinyPNG)
	var uploaded struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	rec := createRunJSON(t, a, cookie, map[string]any{
		"projectId":   "demo-obby",
		"prompt":      "What is in this screenshot?",
		"attachments": []string{uploaded.Path},
	})
	if rec.Code != 201 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var run models.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.PromptSnapshot, "## Attached images") || !strings.Contains(run.PromptSnapshot, uploaded.Path) {
		t.Errorf("PromptSnapshot=%q, want an attached-images block naming %q", run.PromptSnapshot, uploaded.Path)
	}
	if !strings.Contains(run.PromptSnapshot, "What is in this screenshot?") {
		t.Errorf("PromptSnapshot=%q, want the operator's own text preserved", run.PromptSnapshot)
	}
}

func TestCreateRunRejectsAttachmentOutsideProject(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := createRunJSON(t, a, cookie, map[string]any{
		"projectId":   "demo-obby",
		"prompt":      "Look at this",
		"attachments": []string{".studioforge/attachments/../../../secret.txt"},
	})
	if rec.Code != 400 {
		t.Fatalf("a bogus attachment path must be refused, status=%d body=%s", rec.Code, rec.Body.String())
	}
}
