package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

func TestConversationAppendAndLoadOrdersMessages(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, err := store.CreateProject(ctx, models.Project{Name: "Convo project", Path: filepath.Join(t.TempDir(), "convo-project"), Fingerprint: "convo-project"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := store.CreateThread(ctx, project.ID, "Chat")
	if err != nil {
		t.Fatal(err)
	}

	msgs := []models.ConversationMessage{
		{ThreadID: thread.ID, RunID: "run-1", Role: "user", Content: "hello"},
		{ThreadID: thread.ID, RunID: "run-1", Role: "assistant", Content: "hi there", ToolCallsJSON: `[{"id":"call_0"}]`, Model: "test-model"},
		{ThreadID: thread.ID, RunID: "run-1", Role: "tool", ToolCallID: "call_0", Content: "tool result"},
	}
	for _, m := range msgs {
		if err := store.AppendConversationMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	loaded, err := store.LoadConversation(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded=%d, want 3", len(loaded))
	}
	if loaded[0].Role != "user" || loaded[0].Content != "hello" {
		t.Errorf("loaded[0]=%+v", loaded[0])
	}
	if loaded[1].Role != "assistant" || loaded[1].ToolCallsJSON != `[{"id":"call_0"}]` || loaded[1].Model != "test-model" {
		t.Errorf("loaded[1]=%+v", loaded[1])
	}
	if loaded[2].Role != "tool" || loaded[2].ToolCallID != "call_0" || loaded[2].Content != "tool result" {
		t.Errorf("loaded[2]=%+v", loaded[2])
	}
	for i := 1; i < len(loaded); i++ {
		if loaded[i].ID <= loaded[i-1].ID {
			t.Fatalf("messages not in ascending id order: %+v", loaded)
		}
	}
}

func TestConversationLoadEmptyThreadReturnsEmptySlice(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	loaded, err := store.LoadConversation(ctx, "no-such-thread")
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || len(loaded) != 0 {
		t.Fatalf("loaded=%+v, want empty non-nil slice", loaded)
	}
}

func TestConversationSecondThreadStaysIsolated(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, err := store.CreateProject(ctx, models.Project{Name: "Isolation project", Path: filepath.Join(t.TempDir(), "isolation-project"), Fingerprint: "isolation-project"})
	if err != nil {
		t.Fatal(err)
	}
	threadA, err := store.CreateThread(ctx, project.ID, "Thread A")
	if err != nil {
		t.Fatal(err)
	}
	threadB, err := store.CreateThread(ctx, project.ID, "Thread B")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendConversationMessage(ctx, models.ConversationMessage{ThreadID: threadA.ID, Role: "user", Content: "in A"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendConversationMessage(ctx, models.ConversationMessage{ThreadID: threadB.ID, Role: "user", Content: "in B"}); err != nil {
		t.Fatal(err)
	}

	loadedA, err := store.LoadConversation(ctx, threadA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedA) != 1 || loadedA[0].Content != "in A" {
		t.Fatalf("loadedA=%+v", loadedA)
	}
	loadedB, err := store.LoadConversation(ctx, threadB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedB) != 1 || loadedB[0].Content != "in B" {
		t.Fatalf("loadedB=%+v", loadedB)
	}
}

func TestConversationAttachmentsRoundTrip(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	project, err := store.CreateProject(ctx, models.Project{Name: "Attachment project", Path: filepath.Join(t.TempDir(), "attachment-project"), Fingerprint: "attachment-project"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := store.CreateThread(ctx, project.ID, "Chat")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendConversationMessage(ctx, models.ConversationMessage{ThreadID: thread.ID, Role: "user", Content: "see attached", Attachments: []string{"a.png", "b.png"}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadConversation(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || len(loaded[0].Attachments) != 2 || loaded[0].Attachments[0] != "a.png" || loaded[0].Attachments[1] != "b.png" {
		t.Fatalf("loaded=%+v", loaded)
	}
}
