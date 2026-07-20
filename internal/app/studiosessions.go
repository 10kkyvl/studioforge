package app

import (
	"context"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
	"github.com/10kkyvl/studioforge/internal/roblox/studio"
)

// resolveSessionProjects matches each discovered Studio instance against a
// registered project's expected place name — the same PlaceName rule
// Provision/Validate already match on — so an unambiguous match can auto-bind
// a brand-new instance. It is a pure function so the matching rule itself can
// be tested without a live launcher or database; UpsertRealStudioSessions is
// what guarantees an existing manual binding is never overridden by whatever
// this resolves on a later pass.
func resolveSessionProjects(instances []mcp.Session, projectList []models.Project) []models.StudioSession {
	placeToProject := make(map[string]string, len(projectList))
	for _, project := range projectList {
		placeToProject[studio.PlaceName(project.Name, project.ID)] = project.ID
	}
	sessions := make([]models.StudioSession, 0, len(instances))
	for _, instance := range instances {
		sessions = append(sessions, models.StudioSession{
			InstanceID: instance.InstanceID,
			Name:       instance.Name,
			Active:     instance.Active,
			PlayState:  instance.PlayState,
			ProjectID:  placeToProject[instance.Name],
		})
	}
	return sessions
}

// studioSessionStore is the narrow slice of *database.Store the refresher
// needs, so it can be tested without a real database.
type studioSessionStore interface {
	ListProjects(ctx context.Context, includeArchived bool) ([]models.Project, error)
	UpsertRealStudioSessions(ctx context.Context, sessions []models.StudioSession) error
}

// studioSessionsRefresher builds the daemon-side probe that discovers real
// Studio instances and persists them. Detected, not the presence of
// instances, is what the caller reports onward as "Studio MCP not detected"
// versus "detected, nothing open" — an absent launcher, or one that answered
// but reported nothing, are different things to tell an operator.
func studioSessionsRefresher(provisioner *mcp.Provisioner, store studioSessionStore) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		found, err := provisioner.ListSessions(ctx)
		if err != nil {
			return found.Detected, err
		}
		if !found.Detected {
			return false, nil
		}
		projectList, err := store.ListProjects(ctx, false)
		if err != nil {
			return found.Detected, err
		}
		return true, store.UpsertRealStudioSessions(ctx, resolveSessionProjects(found.Instances, projectList))
	}
}
