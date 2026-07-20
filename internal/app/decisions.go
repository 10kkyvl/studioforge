package app

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/scheduler"
)

// decisionStore is the narrow slice of *database.Store the proposer needs, so
// it can be tested without a real database.
type decisionStore interface {
	CreateDecision(ctx context.Context, decision models.Decision) (models.Decision, error)
}

// decisionProposer builds the hook that persists a pending Decision instead
// of letting a failed validation's exhausted correction budget silently give
// up. The proposed correction Job is serialized as-is, so the resolve
// endpoint can submit it verbatim if the operator approves.
func decisionProposer(store decisionStore) scheduler.DecisionProposer {
	return func(ctx context.Context, runID, projectID, summary, detail string, correction scheduler.Job) {
		payload, err := json.Marshal(correction)
		if err != nil {
			slog.Error("failed to serialize proposed correction run", "run_id", runID, "error", err)
			return
		}
		if _, err := store.CreateDecision(ctx, models.Decision{ProjectID: projectID, RunID: runID, Kind: "correction_run", Summary: summary, Detail: detail, Payload: string(payload)}); err != nil {
			slog.Error("failed to persist proposed decision", "run_id", runID, "error", err)
		}
	}
}
