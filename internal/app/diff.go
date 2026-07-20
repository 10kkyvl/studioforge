package app

import (
	"context"

	"github.com/10kkyvl/studioforge/internal/gitops"
)

type diffAdapter struct{ client *gitops.Client }

func (a *diffAdapter) DiffHead(ctx context.Context, projectPath string) (string, error) {
	return a.client.DiffHead(ctx, projectPath)
}
