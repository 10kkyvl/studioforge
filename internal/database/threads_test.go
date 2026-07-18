package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

func newThreadStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "threads.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := NewStore(db)
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	return store, ctx
}

func TestEnsureDefaultThreadIsStable(t *testing.T) {
	store, ctx := newThreadStore(t)
	first, err := store.EnsureDefaultThread(ctx, "demo-obby")
	if err != nil || first.ID == "" {
		t.Fatalf("ensure thread: %v id=%q", err, first.ID)
	}
	second, err := store.EnsureDefaultThread(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Errorf("default thread must be reused, got %q then %q", first.ID, second.ID)
	}
}

func TestListAgentsCarriesSystemPrompt(t *testing.T) {
	store, ctx := newThreadStore(t)
	agents, err := store.ListAgents(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) == 0 {
		t.Fatal("demo project should have agents")
	}
	for _, a := range agents {
		if a.SystemPrompt == "" {
			t.Errorf("agent %q has no system prompt; its role never reaches the model", a.Name)
		}
	}
}

func TestLatestThreadSessionSelfHeals(t *testing.T) {
	store, ctx := newThreadStore(t)
	thread, err := store.EnsureDefaultThread(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	r1, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced", ThreadID: thread.ID}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateRun(ctx, r1.ID, "completed", "verified", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRunUsage(ctx, r1.ID, "sess-A", 0, models.TokenUsage{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.LatestThreadSession(ctx, thread.ID); got != "sess-A" {
		t.Fatalf("a completed run's session must be resumable, got %q", got)
	}
	// A later failed run means the next message must start fresh, so a dead
	// session cannot wedge the thread into resuming it forever.
	r2, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced", ThreadID: thread.ID}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateRun(ctx, r2.ID, "failed", "failed", "", "boom"); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.LatestThreadSession(ctx, thread.ID); got != "" {
		t.Errorf("after a failed run the thread must start fresh, got %q", got)
	}
}

func TestLatestThreadSessionEmptyWhenNoRuns(t *testing.T) {
	store, ctx := newThreadStore(t)
	thread, err := store.EnsureDefaultThread(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.LatestThreadSession(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session != "" {
		t.Errorf("a thread with no runs must resume nothing, got %q", session)
	}
}

func TestCreateAndListThreads(t *testing.T) {
	store, ctx := newThreadStore(t)
	if _, err := store.EnsureDefaultThread(ctx, "demo-obby"); err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateThread(ctx, "demo-obby", "Ideas")
	if err != nil {
		t.Fatal(err)
	}
	if created.Title != "Ideas" {
		t.Errorf("title=%q want %q", created.Title, "Ideas")
	}
	if created.ProjectID != "demo-obby" {
		t.Errorf("projectID=%q want demo-obby", created.ProjectID)
	}
	list, err := store.ListThreads(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 {
		t.Fatalf("expected at least 2 threads, got %d", len(list))
	}
	if list[0].ID != created.ID {
		t.Errorf("newest thread must be first, got %+v", list[0])
	}

	blank, err := store.CreateThread(ctx, "demo-obby", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if blank.Title != "New chat" {
		t.Errorf("blank title must default to %q, got %q", "New chat", blank.Title)
	}
}

func TestThreadByIDReturnsProject(t *testing.T) {
	store, ctx := newThreadStore(t)
	created, err := store.CreateThread(ctx, "demo-obby", "Ideas")
	if err != nil {
		t.Fatal(err)
	}
	found, err := store.ThreadByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.ProjectID != "demo-obby" {
		t.Errorf("projectID=%q want demo-obby", found.ProjectID)
	}
	if found.Title != "Ideas" {
		t.Errorf("title=%q want %q", found.Title, "Ideas")
	}
}

func TestThreadMessagesAssemblesTranscript(t *testing.T) {
	store, ctx := newThreadStore(t)
	thread, err := store.CreateThread(ctx, "demo-obby", "Build")
	if err != nil {
		t.Fatal(err)
	}
	first, ok, err := store.CreateRun(ctx, models.Run{
		ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced",
		ThreadID: thread.ID, PromptSnapshot: "first",
	}, "")
	if err != nil || !ok {
		t.Fatalf("create first run: %v ok=%v", err, ok)
	}
	if _, err := store.AppendEvents(ctx, []models.RunEvent{{
		ProjectID: "demo-obby", RunID: first.ID, AgentID: "demo-obby-orch", Type: "message",
		Payload: map[string]string{"message": "agent reply"},
	}}); err != nil {
		t.Fatal(err)
	}
	second, ok, err := store.CreateRun(ctx, models.Run{
		ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced",
		ThreadID: thread.ID, PromptSnapshot: "second",
	}, "")
	if err != nil || !ok {
		t.Fatalf("create second run: %v ok=%v", err, ok)
	}

	messages, err := store.ThreadMessages(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages (user,agent,user), got %d: %+v", len(messages), messages)
	}
	if messages[0].Role != "user" || messages[0].Text != "first" {
		t.Errorf("messages[0]=%+v, want user/first", messages[0])
	}
	if messages[0].RunID != first.ID {
		t.Errorf("messages[0].RunID=%q want %q", messages[0].RunID, first.ID)
	}
	agentIndex := -1
	for i, m := range messages {
		if m.Role == "agent" {
			agentIndex = i
			break
		}
	}
	if agentIndex == -1 {
		t.Fatalf("no agent message found: %+v", messages)
	}
	if messages[agentIndex].Text != "agent reply" {
		t.Errorf("agent message text=%q want %q", messages[agentIndex].Text, "agent reply")
	}
	if agentIndex <= 0 {
		t.Errorf("agent message must appear after its user message, got index %d", agentIndex)
	}
	last := messages[len(messages)-1]
	if last.Role != "user" || last.Text != "second" || last.RunID != second.ID {
		t.Errorf("last message=%+v, want user/second/%q", last, second.ID)
	}
}

func TestProjectSettingRoundTrip(t *testing.T) {
	store, ctx := newThreadStore(t)
	if err := store.SetProjectSetting(ctx, "demo-obby", "lead_agent_id", "x"); err != nil {
		t.Fatal(err)
	}
	value, ok, err := store.ProjectSetting(ctx, "demo-obby", "lead_agent_id")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || value != "x" {
		t.Fatalf("value=%q ok=%v want %q true", value, ok, "x")
	}
	if _, ok, err := store.ProjectSetting(ctx, "demo-obby", "unknown_key"); err != nil || ok {
		t.Fatalf("unknown key should be ok=false, got ok=%v err=%v", ok, err)
	}
}

func TestTypicalRunSeconds(t *testing.T) {
	store, ctx := newThreadStore(t)
	seconds, samples, err := store.TypicalRunSeconds(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	if samples < 1 {
		t.Fatalf("demo seed has a completed history run, want samples>=1, got %d", samples)
	}
	if seconds < 0 {
		t.Errorf("seconds=%v want >=0", seconds)
	}
	if _, samples, err := store.TypicalRunSeconds(ctx, "unknown-project"); err != nil {
		t.Fatal(err)
	} else if samples != 0 {
		t.Errorf("unknown project should have samples=0, got %d", samples)
	}
}

func TestCreateTaskDefaultsAndAppearsInList(t *testing.T) {
	store, ctx := newThreadStore(t)
	created, err := store.CreateTask(ctx, models.Task{ProjectID: "demo-obby", Title: "New feature"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("created task must have an id")
	}
	if created.Status != "backlog" {
		t.Errorf("status=%q want %q", created.Status, "backlog")
	}
	if created.Priority != 50 {
		t.Errorf("priority=%d want 50", created.Priority)
	}
	tasks, err := store.ListTasks(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, task := range tasks {
		if task.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("created task %q not found in list %+v", created.ID, tasks)
	}
}

func TestUpdateTaskChangesStatus(t *testing.T) {
	store, ctx := newThreadStore(t)
	created, err := store.CreateTask(ctx, models.Task{ProjectID: "demo-obby", Title: "Ship it", Priority: 60})
	if err != nil {
		t.Fatal(err)
	}
	created.Status = "running"
	updated, err := store.UpdateTask(ctx, created)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "running" {
		t.Errorf("status=%q want %q", updated.Status, "running")
	}
	fetched, err := store.Task(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Status != "running" {
		t.Errorf("fetched status=%q want %q", fetched.Status, "running")
	}
}

func TestDeleteTaskRemovesIt(t *testing.T) {
	store, ctx := newThreadStore(t)
	created, err := store.CreateTask(ctx, models.Task{ProjectID: "demo-obby", Title: "Temp task"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteTask(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Task(ctx, created.ID); err == nil {
		t.Fatal("deleted task must not be found")
	}
	tasks, err := store.ListTasks(ctx, "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.ID == created.ID {
			t.Errorf("deleted task %q still present in list", created.ID)
		}
	}
}

func TestSetTaskStatus(t *testing.T) {
	store, ctx := newThreadStore(t)
	created, err := store.CreateTask(ctx, models.Task{ProjectID: "demo-obby", Title: "Attach me"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetTaskStatus(ctx, created.ID, "running"); err != nil {
		t.Fatal(err)
	}
	fetched, err := store.Task(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Status != "running" {
		t.Errorf("status=%q want %q", fetched.Status, "running")
	}
}

func TestAgentEventText(t *testing.T) {
	cases := []struct {
		payload string
		want    string
	}{
		{`{"text":"a"}`, "a"},
		{`{"message":"b"}`, "b"},
		{`{"message":{"content":[{"type":"text","text":"c"}]}}`, "c"},
		{`{"type":"system"}`, ""},
	}
	for _, c := range cases {
		if got := agentEventText(c.payload); got != c.want {
			t.Errorf("agentEventText(%s) = %q, want %q", c.payload, got, c.want)
		}
	}
}
