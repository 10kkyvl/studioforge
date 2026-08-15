package gitops

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/10kkyvl/studioforge/internal/gitcheckpoint"
)

var ErrInvalidRef = errors.New("invalid git ref")

var ErrInvalidTagName = errors.New("invalid tag name")

func validateRef(ref string) error {
	if ref == "" {
		return ErrInvalidRef
	}
	if len(ref) > 255 {
		return ErrInvalidRef
	}
	if strings.HasPrefix(ref, "-") {
		return ErrInvalidRef
	}
	if strings.Contains(ref, "..") || strings.Contains(ref, "@{") {
		return ErrInvalidRef
	}
	for _, r := range ref {
		if !isValidRefRune(r) {
			return ErrInvalidRef
		}
	}
	return nil
}

func isValidRefRune(r rune) bool {
	switch {
	case r >= '0' && r <= '9':
		return true
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	}
	switch r {
	case '.', '_', '/', '^', '{', '}', '~', '-':
		return true
	}
	return false
}

func validateTagName(name string) error {
	if name == "" {
		return ErrInvalidTagName
	}
	if len(name) > 128 {
		return ErrInvalidTagName
	}
	if strings.HasPrefix(name, "-") {
		return ErrInvalidTagName
	}
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return ErrInvalidTagName
	}
	if strings.HasSuffix(name, ".lock") {
		return ErrInvalidTagName
	}
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		return ErrInvalidTagName
	}
	for _, r := range name {
		if r <= ' ' || r == 0x7f {
			return ErrInvalidTagName
		}
	}
	for _, r := range []rune{'~', '^', ':', '?', '*', '[', '\\'} {
		if strings.ContainsRune(name, r) {
			return ErrInvalidTagName
		}
	}
	return nil
}

type Client struct{ Executable string }

func New() *Client { return &Client{Executable: "git"} }
func (c *Client) run(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.Executable, args...)
	cmd.Dir = root
	cmd.Env = gitcheckpoint.ScrubbedEnvironment()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
func (c *Client) runStdin(ctx context.Context, root, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.Executable, args...)
	cmd.Dir = root
	cmd.Env = gitcheckpoint.ScrubbedEnvironment()
	cmd.Stdin = strings.NewReader(stdin)
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
	if err := validateRef(commit); err != nil {
		return "", err
	}
	if _, err := c.run(ctx, root, "rev-parse", "--git-dir"); err != nil {
		return "", nil
	}
	return c.run(ctx, root, "diff", commit)
}

// ErrUnknownRef is returned by DiffRange when either ref does not resolve to
// a commit in the project's repository.
var ErrUnknownRef = errors.New("unknown git ref")

// ErrNotAncestor is returned by DiffRange when from is not an ancestor of to,
// so a range diff between them would not describe a coherent forward change.
var ErrNotAncestor = errors.New("from is not an ancestor of to")

// DiffRange returns the diff between two arbitrary refs (commit hashes or
// "HEAD"), after validating both resolve to real commits and that from is an
// ancestor of to. Like DiffHead/DiffCommit it returns "" cleanly, with no
// error, when root is not a git repository at all.
func (c *Client) DiffRange(ctx context.Context, root, from, to string) (string, error) {
	for _, ref := range []string{from, to} {
		if err := validateRef(ref); err != nil {
			return "", err
		}
	}
	if _, err := c.run(ctx, root, "rev-parse", "--git-dir"); err != nil {
		return "", nil
	}
	for _, ref := range []string{from, to} {
		if _, err := c.run(ctx, root, "cat-file", "-e", ref+"^{commit}"); err != nil {
			return "", fmt.Errorf("%w: %s", ErrUnknownRef, ref)
		}
	}
	if _, err := c.run(ctx, root, "merge-base", "--is-ancestor", from, to); err != nil {
		return "", fmt.Errorf("%w: %s is not an ancestor of %s", ErrNotAncestor, from, to)
	}
	return c.run(ctx, root, "diff", from, to)
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
	if err := validateRef(target); err != nil {
		return "", err
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
	if err := validateTagName(name); err != nil {
		return err
	}
	_, err := c.run(ctx, root, "tag", "-a", "-m", "StudioForge milestone "+name, "--", name)
	return err
}
