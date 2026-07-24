package tasks

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

type fakeReadinessStore struct {
	tasks map[string]models.Task
}

func (f fakeReadinessStore) Task(_ context.Context, id string) (models.Task, error) {
	t, ok := f.tasks[id]
	if !ok {
		return models.Task{}, sql.ErrNoRows
	}
	return t, nil
}

func TestTaskReadinessNoDependencies(t *testing.T) {
	store := fakeReadinessStore{tasks: map[string]models.Task{
		"a": {ID: "a", ProjectID: "p1", Title: "A", Status: "backlog"},
	}}
	ready, blockers, err := TaskReadiness(context.Background(), store, "p1", "a")
	if err != nil {
		t.Fatal(err)
	}
	if !ready || len(blockers) != 0 {
		t.Fatalf("ready=%v blockers=%v", ready, blockers)
	}
}

func TestTaskReadinessOneCompletedDependency(t *testing.T) {
	store := fakeReadinessStore{tasks: map[string]models.Task{
		"a": {ID: "a", ProjectID: "p1", Title: "A", Status: "completed"},
		"b": {ID: "b", ProjectID: "p1", Title: "B", Status: "backlog", Dependencies: []string{"a"}},
	}}
	ready, blockers, err := TaskReadiness(context.Background(), store, "p1", "b")
	if err != nil {
		t.Fatal(err)
	}
	if !ready || len(blockers) != 0 {
		t.Fatalf("ready=%v blockers=%v", ready, blockers)
	}
}

func TestTaskReadinessOneIncompleteDependency(t *testing.T) {
	store := fakeReadinessStore{tasks: map[string]models.Task{
		"a": {ID: "a", ProjectID: "p1", Title: "A", Status: "running"},
		"b": {ID: "b", ProjectID: "p1", Title: "B", Status: "backlog", Dependencies: []string{"a"}},
	}}
	ready, blockers, err := TaskReadiness(context.Background(), store, "p1", "b")
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("expected not ready")
	}
	if len(blockers) != 1 || blockers[0].TaskID != "a" || blockers[0].Status != "running" || blockers[0].Title != "A" {
		t.Fatalf("blockers=%+v", blockers)
	}
}

func TestTaskReadinessTransitiveIncompleteDependency(t *testing.T) {
	store := fakeReadinessStore{tasks: map[string]models.Task{
		"a": {ID: "a", ProjectID: "p1", Title: "A", Status: "running"},
		"b": {ID: "b", ProjectID: "p1", Title: "B", Status: "completed", Dependencies: []string{"a"}},
		"c": {ID: "c", ProjectID: "p1", Title: "C", Status: "backlog", Dependencies: []string{"b"}},
	}}
	ready, blockers, err := TaskReadiness(context.Background(), store, "p1", "c")
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("expected not ready")
	}
	if len(blockers) != 1 || blockers[0].TaskID != "a" {
		t.Fatalf("blockers=%+v", blockers)
	}
}

func TestTaskReadinessMissingDependency(t *testing.T) {
	store := fakeReadinessStore{tasks: map[string]models.Task{
		"b": {ID: "b", ProjectID: "p1", Title: "B", Status: "backlog", Dependencies: []string{"deleted"}},
	}}
	ready, blockers, err := TaskReadiness(context.Background(), store, "p1", "b")
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("expected not ready")
	}
	if len(blockers) != 1 || blockers[0].TaskID != "deleted" || blockers[0].Status != MissingDependencyStatus {
		t.Fatalf("blockers=%+v", blockers)
	}
}

func TestTaskReadinessCrossProjectDependencyRejected(t *testing.T) {
	store := fakeReadinessStore{tasks: map[string]models.Task{
		"a": {ID: "a", ProjectID: "p2", Title: "Other project secret", Status: "completed"},
		"b": {ID: "b", ProjectID: "p1", Title: "B", Status: "backlog", Dependencies: []string{"a"}},
	}}
	ready, blockers, err := TaskReadiness(context.Background(), store, "p1", "b")
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("expected not ready")
	}
	if len(blockers) != 1 || blockers[0].TaskID != "a" || blockers[0].Status != MissingDependencyStatus {
		t.Fatalf("blockers=%+v", blockers)
	}
	if blockers[0].Title != "" {
		t.Fatalf("cross-project task title leaked: %+v", blockers[0])
	}
}

func TestTaskReadinessRootTaskFromWrongProjectErrors(t *testing.T) {
	store := fakeReadinessStore{tasks: map[string]models.Task{
		"a": {ID: "a", ProjectID: "p2", Title: "A", Status: "backlog"},
	}}
	if _, _, err := TaskReadiness(context.Background(), store, "p1", "a"); err == nil {
		t.Fatal("expected error for cross-project root task")
	}
}

func TestTaskReadinessCycleDoesNotHang(t *testing.T) {
	store := fakeReadinessStore{tasks: map[string]models.Task{
		"a": {ID: "a", ProjectID: "p1", Title: "A", Status: "backlog", Dependencies: []string{"b"}},
		"b": {ID: "b", ProjectID: "p1", Title: "B", Status: "backlog", Dependencies: []string{"a"}},
	}}
	ready, blockers, err := TaskReadiness(context.Background(), store, "p1", "a")
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("expected not ready")
	}
	if len(blockers) == 0 {
		t.Fatal("expected at least one blocker")
	}
}

func TestTaskReadinessRootTaskMissingErrors(t *testing.T) {
	store := fakeReadinessStore{tasks: map[string]models.Task{}}
	_, _, err := TaskReadiness(context.Background(), store, "p1", "missing-root")
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}
