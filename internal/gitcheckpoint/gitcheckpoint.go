// Package gitcheckpoint commits a project's current state before an agent run,
// so the operator can revert an agent's changes with git. Everything is
// best-effort: a project that is not a git repo, or has nothing to commit, is a
// silent no-op, and StudioForge never fails a run over a checkpoint.
package gitcheckpoint

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Checkpoint commits the working tree at root and returns the new commit
// hash and the branch it was committed to. It returns ("", "", nil) when root
// is not a git repo or there is nothing to commit.
func Checkpoint(root, label string) (hash string, branch string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if run(ctx, root, "rev-parse", "--git-dir") != nil {
		return "", "", nil // not a git repository
	}
	_ = run(ctx, root, "add", "-A")
	status, _ := output(ctx, root, "status", "--porcelain")
	if status == "" {
		return "", "", nil // nothing changed since the last checkpoint
	}
	// -c identity keeps the commit working even when the repo has no configured
	// author, without touching the operator's global git config.
	if err := run(ctx, root,
		"-c", "user.name=StudioForge",
		"-c", "user.email=noreply@studioforge.local",
		"commit", "-m", label,
	); err != nil {
		return "", "", err
	}
	hash, err = output(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	branch, _ = output(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	return hash, branch, nil
}

func run(ctx context.Context, root string, args ...string) error {
	return exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).Run()
}

func output(ctx context.Context, root string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}
