package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/scheduler"
)

type fakeDecisionStore struct {
	created []models.Decision
	err     error
}

func (f *fakeDecisionStore) CreateDecision(_ context.Context, decision models.Decision) (models.Decision, error) {
	if f.err != nil {
		return models.Decision{}, f.err
	}
	f.created = append(f.created, decision)
	return decision, nil
}

func TestDecisionProposerPersistsTheProposedCorrectionAsJSON(t *testing.T) {
	store := &fakeDecisionStore{}
	propose := decisionProposer(store)
	correction := scheduler.Job{ProjectID: "p1", AgentID: "a1", Provider: "claude", Prompt: "fix the bug", ParentRunID: "run-1", CorrectionDepth: 1}

	propose(context.Background(), "run-1", "p1", "Correction run proposed", "still broken", correction)

	if len(store.created) != 1 {
		t.Fatalf("created=%d, want 1", len(store.created))
	}
	got := store.created[0]
	if got.RunID != "run-1" || got.ProjectID != "p1" || got.Kind != "correction_run" {
		t.Errorf("decision=%+v", got)
	}
	if got.Summary != "Correction run proposed" || got.Detail != "still broken" {
		t.Errorf("decision=%+v, want the summary/detail passed through", got)
	}
	var roundTripped scheduler.Job
	if err := json.Unmarshal([]byte(got.Payload), &roundTripped); err != nil {
		t.Fatal(err)
	}
	if roundTripped.Prompt != "fix the bug" || roundTripped.ParentRunID != "run-1" || roundTripped.CorrectionDepth != 1 {
		t.Errorf("roundTripped=%+v, want the correction Job preserved exactly", roundTripped)
	}
}

// A store failure must not panic or propagate up into the scheduler's own
// validation goroutine — it is only logged, the same fail-soft posture every
// other hook in this file takes on a persistence error.
func TestDecisionProposerToleratesAStoreFailure(t *testing.T) {
	store := &fakeDecisionStore{err: context.DeadlineExceeded}
	propose := decisionProposer(store)
	propose(context.Background(), "run-1", "p1", "summary", "detail", scheduler.Job{})
}
