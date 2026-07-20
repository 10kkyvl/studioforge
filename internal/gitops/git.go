package gitops

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Client struct{ Executable string }

func New() *Client { return &Client{Executable: "git"} }
func (c *Client) run(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.Executable, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
func (c *Client) Detect(ctx context.Context, root string) (bool, error) {
	_, err := c.run(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return false, nil
	}
	return true, nil
}
func (c *Client) Init(ctx context.Context, root string) error {
	_, err := c.run(ctx, root, "init")
	return err
}
func (c *Client) Status(ctx context.Context, root string) (string, error) {
	return c.run(ctx, root, "status", "--short", "--branch")
}
func (c *Client) Diff(ctx context.Context, root string) (string, error) {
	return c.run(ctx, root, "diff", "--no-ext-diff")
}
func (c *Client) DiffHead(ctx context.Context, root string) (string, error) {
	if _, err := c.run(ctx, root, "rev-parse", "--git-dir"); err != nil {
		return "", nil
	}
	return c.run(ctx, root, "diff", "HEAD")
}
func (c *Client) DiffCommit(ctx context.Context, root, commit string) (string, error) {
	if _, err := c.run(ctx, root, "rev-parse", "--git-dir"); err != nil {
		return "", nil
	}
	return c.run(ctx, root, "diff", commit)
}
func (c *Client) Checkpoint(ctx context.Context, root, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", errors.New("checkpoint message is required")
	}
	if _, err := c.run(ctx, root, "add", "--all"); err != nil {
		return "", err
	}
	if _, err := c.run(ctx, root, "diff", "--cached", "--check"); err != nil {
		return "", err
	}
	if _, err := c.run(ctx, root, "commit", "-m", message); err != nil {
		return "", err
	}
	return c.run(ctx, root, "rev-parse", "HEAD")
}
func (c *Client) SafeRollback(ctx context.Context, root, target string) (string, error) {
	if target == "" {
		return "", errors.New("target commit is required")
	}
	if _, err := c.run(ctx, root, "cat-file", "-e", target+"^{commit}"); err != nil {
		return "", err
	}
	branch := "studioforge/rollback-" + time.Now().UTC().Format("20060102-150405")
	if _, err := c.run(ctx, root, "switch", "-c", branch, target); err != nil {
		return "", fmt.Errorf("create non-destructive rollback branch: %w", err)
	}
	return branch, nil
}
func (c *Client) Tag(ctx context.Context, root, name string) error {
	if name == "" {
		return errors.New("tag name is required")
	}
	_, err := c.run(ctx, root, "tag", "-a", name, "-m", "StudioForge milestone "+name)
	return err
}
