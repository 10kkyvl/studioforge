package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

func createRunJSON(t *testing.T, a *testAPI, cookie *http.Cookie, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "http://127.0.0.1:1234/api/v1/runs", bytes.NewReader(raw))
	req.Header.Set("Origin", "http://127.0.0.1:1234")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", newRunKey())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

func TestCreateRunRejectsEmptyPrompt(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := createRunJSON(t, a, cookie, map[string]any{"projectId": "demo-obby", "prompt": "  "})
	if rec.Code != 400 {
		t.Fatalf("empty prompt must be refused, status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateRunUsesPromptAndDefaultThread(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := createRunJSON(t, a, cookie, map[string]any{"projectId": "demo-obby", "prompt": "Build me a neon lobby"})
	if rec.Code != 201 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var first models.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.PromptSnapshot != "Build me a neon lobby" {
		t.Errorf("createRun must forward the operator's prompt, got %q", first.PromptSnapshot)
	}
	if first.ThreadID == "" {
		t.Error("a chat run must be linked to a thread")
	}
	second := createRunJSON(t, a, cookie, map[string]any{"projectId": "demo-obby", "prompt": "Now add a spawn"})
	var next models.Run
	if err := json.Unmarshal(second.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if next.ThreadID != first.ThreadID {
		t.Errorf("follow-up messages must reuse the project's thread: %q vs %q", first.ThreadID, next.ThreadID)
	}
}

func TestCreateRunAcceptsPlanMode(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := createRunJSON(t, a, cookie, map[string]any{"projectId": "demo-obby", "prompt": "Plan the lobby", "mode": "plan"})
	if rec.Code != 201 {
		t.Fatalf("plan mode run must be accepted, status=%d body=%s", rec.Code, rec.Body.String())
	}
}

var runKeyCounter int

func newRunKey() string {
	runKeyCounter++
	return "chat-key-" + string(rune('a'+runKeyCounter))
}

func getJSON(t *testing.T, a *testAPI, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "http://127.0.0.1:1234"+path, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

func postJSON(t *testing.T, a *testAPI, cookie *http.Cookie, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "http://127.0.0.1:1234"+path, bytes.NewReader(raw))
	req.Header.Set("Origin", "http://127.0.0.1:1234")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

func TestListThreadsAlwaysHasDefault(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/threads")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Threads []models.ChatThread `json:"threads"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Threads) < 1 {
		t.Fatalf("expected at least 1 thread, got %d", len(body.Threads))
	}
}

func TestCreateThreadThenListed(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/threads", map[string]any{"title": "Design"})
	if rec.Code != 201 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created models.ChatThread
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Title != "Design" {
		t.Errorf("title=%q want %q", created.Title, "Design")
	}
	listRec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/threads")
	var body struct {
		Threads []models.ChatThread `json:"threads"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, th := range body.Threads {
		if th.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("created thread %q not found in list %+v", created.ID, body.Threads)
	}
}

func TestCreateRunTargetsGivenThread(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	threadRec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/threads", map[string]any{"title": "Targeted"})
	if threadRec.Code != 201 {
		t.Fatalf("create thread status=%d body=%s", threadRec.Code, threadRec.Body.String())
	}
	var thread models.ChatThread
	if err := json.Unmarshal(threadRec.Body.Bytes(), &thread); err != nil {
		t.Fatal(err)
	}
	rec := createRunJSON(t, a, cookie, map[string]any{"projectId": "demo-obby", "prompt": "Use this thread", "threadId": thread.ID})
	if rec.Code != 201 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var run models.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.ThreadID != thread.ID {
		t.Errorf("run.ThreadID=%q want %q", run.ThreadID, thread.ID)
	}
}

func TestSetAndGetLead(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/lead", map[string]any{"agentId": "demo-obby-eng"})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		AgentID string `json:"agentId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.AgentID != "demo-obby-eng" {
		t.Errorf("agentId=%q want %q", body.AgentID, "demo-obby-eng")
	}
	getRec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/lead")
	if getRec.Code != 200 {
		t.Fatalf("status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	var getBody struct {
		AgentID string `json:"agentId"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &getBody); err != nil {
		t.Fatal(err)
	}
	if getBody.AgentID != "demo-obby-eng" {
		t.Errorf("GET agentId=%q want %q", getBody.AgentID, "demo-obby-eng")
	}
	badRec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/lead", map[string]any{"agentId": "demo-tycoon-eng"})
	if badRec.Code != 400 {
		t.Fatalf("setting a lead outside the project must 400, status=%d body=%s", badRec.Code, badRec.Body.String())
	}
}

func TestCreateRunUsesLeadAgent(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	leadRec := postJSON(t, a, cookie, "/api/v1/projects/demo-obby/lead", map[string]any{"agentId": "demo-obby-eng"})
	if leadRec.Code != 200 {
		t.Fatalf("status=%d body=%s", leadRec.Code, leadRec.Body.String())
	}
	rec := createRunJSON(t, a, cookie, map[string]any{"projectId": "demo-obby", "prompt": "Use the lead agent"})
	if rec.Code != 201 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var run models.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.AgentID != "demo-obby-eng" {
		t.Errorf("run.AgentID=%q want %q", run.AgentID, "demo-obby-eng")
	}
}

func TestPaceEndpoint(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	rec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/pace")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		TypicalSeconds float64 `json:"typicalSeconds"`
		Samples        int     `json:"samples"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Samples < 0 {
		t.Errorf("samples=%d want >=0", body.Samples)
	}
}

func TestThreadMessagesEndpoint(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	listRec := getJSON(t, a, cookie, "/api/v1/projects/demo-obby/threads")
	var listBody struct {
		Threads []models.ChatThread `json:"threads"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Threads) < 1 {
		t.Fatalf("expected a default thread, got %+v", listBody.Threads)
	}
	defaultThread := listBody.Threads[0]
	runRec := createRunJSON(t, a, cookie, map[string]any{"projectId": "demo-obby", "prompt": "Hello there"})
	if runRec.Code != 201 {
		t.Fatalf("create run status=%d body=%s", runRec.Code, runRec.Body.String())
	}
	msgRec := getJSON(t, a, cookie, "/api/v1/threads/"+defaultThread.ID+"/messages")
	if msgRec.Code != 200 {
		t.Fatalf("status=%d body=%s", msgRec.Code, msgRec.Body.String())
	}
	var body struct {
		Messages []models.ChatMessage `json:"messages"`
	}
	if err := json.Unmarshal(msgRec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) < 1 {
		t.Fatalf("expected at least 1 message, got %+v", body.Messages)
	}
	if body.Messages[0].Role != "user" || body.Messages[0].Text != "Hello there" {
		t.Errorf("messages[0]=%+v, want user/%q", body.Messages[0], "Hello there")
	}
}
