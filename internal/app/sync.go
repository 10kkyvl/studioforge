package app

import (
	"context"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/rojo"
)

// syncAdapter turns *rojo.Manager's Session (which carries a log channel and
// an unexported process handle, neither of which JSON or internal/api needs)
// into the plain models.SyncStatus the project payload serializes — the same
// job studiostatus.go already does for the MCP launcher's status probe.
type syncAdapter struct{ manager *rojo.Manager }

func (a *syncAdapter) Start(ctx context.Context, projectID, projectFile string) (models.SyncStatus, error) {
	session, err := a.manager.Start(ctx, projectID, projectFile)
	if err != nil {
		return models.SyncStatus{}, err
	}
	return models.SyncStatus{Active: true, Port: session.Port, StartedAt: session.StartedAt, RecentLogs: session.RecentLines()}, nil
}

func (a *syncAdapter) Stop(projectID string) error { return a.manager.Stop(projectID) }

func (a *syncAdapter) Status(projectID string) models.SyncStatus {
	session, ok := a.manager.Session(projectID)
	if !ok {
		return models.SyncStatus{}
	}
	return models.SyncStatus{Active: true, Port: session.Port, StartedAt: session.StartedAt, RecentLogs: session.RecentLines()}
}
