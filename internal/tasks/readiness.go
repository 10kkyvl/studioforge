package tasks

import (
	"context"
	"database/sql"
	"errors"

	"github.com/10kkyvl/studioforge/internal/models"
)

// ReadinessStore is the read access TaskReadiness needs: loading a task, with
// its Dependencies already populated, by ID.
type ReadinessStore interface {
	Task(ctx context.Context, id string) (models.Task, error)
}

// Blocker names one dependency that keeps taskID's run from starting: a
// dependency that does not exist (was deleted, or never did), belongs to a
// different project, or has not reached "completed" yet.
type Blocker struct {
	TaskID string `json:"taskId"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// MissingDependencyStatus is the Blocker.Status reported for a dependency
// that cannot be resolved at all: it was deleted, or it belongs to a
// different project than the task that references it. Either way the
// dependency is invalid, not merely unfinished, so no run may proceed while
// it is still listed.
const MissingDependencyStatus = "missing"

// TaskReadiness walks taskID's dependency graph, direct and transitive, and
// reports whether every dependency has reached "completed". Dependencies are
// project-scoped: a dependency that does not resolve to a task inside
// projectID (deleted, or belonging to another project) is reported as a
// blocker with MissingDependencyStatus, and its title is withheld — a
// cross-project task's details must never leak into another project's
// blocker list — and it is not walked any further. Cycles are tolerated (each
// task is visited at most once) so a pre-existing cycle in stored data can
// never hang this call; new dependencies are expected to already have passed
// ValidateDAG.
func TaskReadiness(ctx context.Context, store ReadinessStore, projectID, taskID string) (bool, []Blocker, error) {
	task, err := store.Task(ctx, taskID)
	if err != nil {
		return false, nil, err
	}
	if task.ProjectID != projectID {
		return false, nil, errors.New("task does not belong to project")
	}

	visited := map[string]bool{taskID: true}
	var blockers []Blocker

	var walk func(id string) error
	walk = func(id string) error {
		if visited[id] {
			return nil
		}
		visited[id] = true
		dep, err := store.Task(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				blockers = append(blockers, Blocker{TaskID: id, Status: MissingDependencyStatus})
				return nil
			}
			return err
		}
		if dep.ProjectID != projectID {
			blockers = append(blockers, Blocker{TaskID: id, Status: MissingDependencyStatus})
			return nil
		}
		if dep.Status != "completed" {
			blockers = append(blockers, Blocker{TaskID: dep.ID, Title: dep.Title, Status: dep.Status})
		}
		for _, next := range dep.Dependencies {
			if err := walk(next); err != nil {
				return err
			}
		}
		return nil
	}

	for _, depID := range task.Dependencies {
		if err := walk(depID); err != nil {
			return false, nil, err
		}
	}
	return len(blockers) == 0, blockers, nil
}
