// Package gitcheckpoint commits a project's current state before an agent run,
// so the operator can revert an agent's changes with git. Everything is
// best-effort: a project that is not a git repo is a silent no-op. A clean
// repository reuses HEAD so every run has a rollback point without extra commits.
package gitcheckpoint

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Checkpoint commits the working tree at root and returns the commit hash and
// branch. A clean tree returns its existing HEAD; a non-git directory or an
// empty repository with no commits returns ("", "", nil).
func Checkpoint(root, label string) (hash string, branch string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if run(ctx, root, "rev-parse", "--git-dir") != nil {
		return "", "", nil // not a git repository
	}
	if err := run(ctx, root, "add", "-A"); err != nil {
		return "", "", err
	}
	status, err := output(ctx, root, "status", "--porcelain")
	if err != nil {
		return "", "", err
	}
	if status == "" {
		hash, err = output(ctx, root, "rev-parse", "--verify", "HEAD")
		if err != nil {
			return "", "", nil // empty repository with no initial commit
		}
		branch, _ = output(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
		return hash, branch, nil
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
