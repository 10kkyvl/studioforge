package scheduler

import (
	"context"
	"github.com/10kkyvl/studioforge/internal/database"
	"github.com/10kkyvl/studioforge/internal/events"
	"github.com/10kkyvl/studioforge/internal/memory"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/mock"
	"github.com/10kkyvl/studioforge/internal/resources"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRememberOutcomeOnlyCompletedUsableResults(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "outcomes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := database.NewStore(db)
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	mem := memory.New(db)
	m := &Manager{store: store, memoryStore: mem}
	for _, tc := range []struct {
		status, validation, mode, answer string
		want                             bool
	}{
		{"completed", "none", "", "Implemented server validation", true},
		{"completed", "passed", "", "Confirmed playtest passes", true},
		{"completed", "failed", "", "Everything is fixed", false},
		{"completed", "inconclusive", "", "Everything is fixed", false},
		{"cancelled", "none", "", "Partial work", false},
		{"completed", "none", "plan", "I will implement it", false},
		{"completed", "none", "", "", false},
	} {
		t.Run(tc.status+tc.validation+tc.mode+tc.answer, func(t *testing.T) {
			run, _, err := store.CreateRun(ctx, models.Run{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", ModelAlias: "balanced"}, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := store.UpdateRun(ctx, run.ID, tc.status, "verified", "", ""); err != nil {
				t.Fatal(err)
			}
			if err := store.SetRunValidation(ctx, run.ID, tc.validation, ""); err != nil {
				t.Fatal(err)
			}
			before, _ := mem.List(ctx, "demo-obby")
			m.rememberOutcome(&Job{ProjectID: "demo-obby", RunID: run.ID, AgentID: "demo-obby-orch", Prompt: "Please fix the issue", Mode: tc.mode}, tc.answer)
			after, err := mem.List(ctx, "demo-obby")
			if err != nil {
				t.Fatal(err)
			}
			if (len(after) > len(before)) != tc.want {
				t.Fatalf("before=%d after=%d want saved=%v", len(before), len(after), tc.want)
			}
			if tc.want && (!strings.Contains(after[0].Content, tc.answer) || after[0].Summary == "Please fix the issue") {
				t.Fatalf("did not save outcome: %+v", after[0])
			}
		})
	}
}

func TestBoundedMemoryTextUTF8(t *testing.T) {
	got := boundedMemoryText(strings.Repeat("ёж", 1000), 601)
	if len(got) > 601 || !utf8.ValidString(got) {
		t.Fatalf("invalid bounded text len=%d", len(got))
	}
}

func TestCompletedRunRemembersFinalNotStreamingText(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := database.NewStore(db)
	data := t.TempDir()
	if err := store.SeedDemo(ctx, data); err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub(store)
	defer hub.Close()
	leases := resources.NewManager(time.Second)
	defer leases.Close()
	provider := mock.New()
	provider.StepDelay = time.Millisecond
	manager := New(ctx, store, hub, leases, map[string]providers.Provider{"mock": provider})
	defer manager.Close(ctx)
	mem := memory.New(db)
	manager.SetMemory(mem)
	run, _, err := manager.Submit(ctx, Job{ProjectID: "demo-obby", AgentID: "demo-obby-orch", Provider: "mock", Model: "balanced", WorkingDirectory: data, Prompt: "unique task prompt", MaxBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, run.ID, "completed", 3*time.Second)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entries, err := mem.List(ctx, "demo-obby")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) > 0 {
			if entries[0].RunID != run.ID || !strings.HasPrefix(entries[0].Content, "Reported outcome:") || strings.Contains(entries[0].Content, "Reading project constitution") {
				t.Fatalf("wrong saved result: %+v", entries[0])
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("completed run did not persist final answer")
}

func TestOutcomeExcerptKeepsVerificationAtEnd(t *testing.T) {
	text := "Решение: серверная проверка. " + strings.Repeat("подробности ", 500) + "Проверка: тесты пройдены; Studio ещё не проверена."
	got := outcomeExcerpt(text, 560)
	if len(got) > 560 || !utf8.ValidString(got) || !strings.Contains(got, "Решение:") || !strings.Contains(got, "Studio ещё не проверена.") {
		t.Fatalf("lost outcome boundaries: %s", got)
	}
}
