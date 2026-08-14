package app

import (
	"context"

	"github.com/10kkyvl/studioforge/internal/gitops"
)

type gitAdapter struct{ client *gitops.Client }

func (a *gitAdapter) DiffHead(ctx context.Context, projectPath string) (string, error) {
	return a.client.DiffHead(ctx, projectPath)
}
func (a *gitAdapter) DiffCommit(ctx context.Context, projectPath, commit string) (string, error) {
	return a.client.DiffCommit(ctx, projectPath, commit)
}
func (a *gitAdapter) DiffRange(ctx context.Context, projectPath, from, to string) (string, error) {
	return a.client.DiffRange(ctx, projectPath, from, to)
}
func (a *gitAdapter) Status(ctx context.Context, projectPath string) (string, error) {
	return a.client.Status(ctx, projectPath)
}
func (a *gitAdapter) SafeRollback(ctx context.Context, projectPath, target string) (string, error) {
	return a.client.SafeRollback(ctx, projectPath, target)
}
func (a *gitAdapter) SelectiveRollback(ctx context.Context, projectPath, checkpoint, nextCheckpoint string, files []string, hunks []gitops.HunkSelection) (gitops.SelectiveRollbackResult, error) {
	return a.client.SelectiveRollback(ctx, projectPath, checkpoint, nextCheckpoint, files, hunks)
}
func (a *gitAdapter) Tag(ctx context.Context, projectPath, name string) error {
	return a.client.Tag(ctx, projectPath, name)
}
