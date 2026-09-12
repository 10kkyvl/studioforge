// Package reviewgate resolves reviewed Git snapshots without sweeping untracked
// files or parsing file names from human-readable diff headers.
package reviewgate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Client struct{}

func New() *Client { return &Client{} }

type Snapshot struct {
	Diff     string   `json:"diff"`
	NewFiles []string `json:"newFiles"`
	Tree     string   `json:"tree"`
	Head     string   `json:"head"`
	Index    string   `json:"index"`
}
type HunkSelection struct {
	Path  string `json:"path"`
	Index int    `json:"index"`
}

func git(ctx context.Context, root, index string, input []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--literal-pathspecs", "-c", "core.hooksPath=", "-c", "core.fsmonitor=false"}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_EXTERNAL_DIFF=", "GIT_DIFF_OPTS=")
	if index != "" {
		cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+index)
	}
	if input != nil {
		cmd.Stdin = strings.NewReader(string(input))
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
func textGit(ctx context.Context, root, index string, args ...string) (string, error) {
	out, err := git(ctx, root, index, nil, args...)
	return strings.TrimSpace(string(out)), err
}
func temporaryIndex() (string, func(), error) {
	file, err := os.CreateTemp("", "studioforge-review-index-")
	if err != nil {
		return "", nil, err
	}
	name := file.Name()
	file.Close()
	os.Remove(name)
	return name, func() { os.Remove(name); os.Remove(name + ".lock") }, nil
}
func ownRepository(ctx context.Context, root, base string) error {
	top, err := textGit(ctx, root, "", "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(top)
	if err != nil {
		return err
	}
	rootInfo, err := os.Stat(canonical)
	if err != nil {
		return err
	}
	topInfo, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if !os.SameFile(rootInfo, topInfo) {
		return errors.New("review_not_project_repository: project must be the Git repository root")
	}
	_, err = textGit(ctx, root, "", "rev-parse", "--verify", "--end-of-options", base+"^{commit}")
	return err
}
func (c *Client) Diff(ctx context.Context, root, base string) (Snapshot, error) {
	if err := ownRepository(ctx, root, base); err != nil {
		return Snapshot{}, err
	}
	head, err := textGit(ctx, root, "", "rev-parse", "HEAD")
	if err != nil {
		return Snapshot{}, err
	}
	actualIndex, err := textGit(ctx, root, "", "write-tree")
	if err != nil {
		return Snapshot{}, err
	}
	index, cleanup, err := temporaryIndex()
	if err != nil {
		return Snapshot{}, err
	}
	defer cleanup()
	if _, err := git(ctx, root, index, nil, "read-tree", head); err != nil {
		return Snapshot{}, err
	}
	if _, err := git(ctx, root, index, nil, "add", "-A", "--", "."); err != nil {
		return Snapshot{}, err
	}
	tree, err := textGit(ctx, root, index, "write-tree")
	if err != nil {
		return Snapshot{}, err
	}
	diff, err := git(ctx, root, "", nil, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", "--full-index", base, tree, "--")
	if err != nil {
		return Snapshot{}, err
	}
	added, err := git(ctx, root, "", nil, "diff", "--name-only", "--diff-filter=A", "-z", base, tree, "--")
	if err != nil {
		return Snapshot{}, err
	}
	files := []string{}
	for _, p := range strings.Split(string(added), "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	return Snapshot{Diff: string(diff), NewFiles: files, Tree: tree, Head: head, Index: actualIndex}, nil
}
func validatePath(path string) (string, error) {
	if path == "" || path == "." || filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", errors.New("selected review path is outside the project")
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, ".git/") || clean == ".git" {
		return "", errors.New("selected review path is outside the project")
	}
	return clean, nil
}
func selectedHunks(patch string, indices []int) (string, error) {
	if strings.Contains(patch, "GIT binary patch") || strings.Contains(patch, "new file mode") || strings.Contains(patch, "deleted file mode") || strings.Contains(patch, "old mode") {
		return "", errors.New("review_hunk_unsupported: select the complete binary, added, deleted, or mode-changing file")
	}
	lines := strings.SplitAfter(patch, "\n")
	header := ""
	blocks := []string{}
	for _, line := range lines {
		if strings.HasPrefix(line, "@@ ") {
			blocks = append(blocks, line)
		} else if len(blocks) == 0 {
			header += line
		} else {
			blocks[len(blocks)-1] += line
		}
	}
	chosen := map[int]bool{}
	for _, i := range indices {
		if i < 0 || i >= len(blocks) {
			return "", fmt.Errorf("review_invalid_hunk: %d", i)
		}
		chosen[i] = true
	}
	out := header
	for i, b := range blocks {
		if chosen[i] {
			out += b
		}
	}
	return out, nil
}
func (c *Client) Resolve(ctx context.Context, root, base string, expected Snapshot, action string, selected []string, hunks []HunkSelection) error {
	if expected.Tree == "" || expected.Head == "" || expected.Index == "" {
		return errors.New("review_snapshot_missing: reopen the review from a fresh run")
	}
	current, err := c.Diff(ctx, root, base)
	if err != nil {
		return err
	}
	if current.Tree != expected.Tree || current.Head != expected.Head || current.Index != expected.Index {
		return errors.New("review_tree_changed: files, index, or HEAD changed after the review was prepared; current files were preserved")
	}
	if action == "apply" || action == "approve" {
		return nil
	}
	if action != "reject" && action != "deny" && action != "apply_selected" && action != "selected" {
		return errors.New("review action must be apply, reject, or apply_selected")
	}
	index, cleanup, err := temporaryIndex()
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := git(ctx, root, index, nil, "read-tree", base); err != nil {
		return err
	}
	if action == "apply_selected" || action == "selected" {
		full := map[string]bool{}
		for _, p := range selected {
			path, err := validatePath(p)
			if err != nil {
				return err
			}
			if full[path] {
				continue
			}
			full[path] = true
			// Each path must actually be a reviewed file; empty/mistyped selections
			// must not silently become whole-repository pathspecs.
			changed, err := git(ctx, root, "", nil, "diff", "--name-only", "-z", base, expected.Tree, "--", path)
			if err != nil {
				return err
			}
			if string(changed) != path+"\x00" {
				return fmt.Errorf("review_file_not_changed: %s", path)
			}
			if _, err := git(ctx, root, index, []byte(path+"\x00"), "restore", "--source="+expected.Tree, "--staged", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
				return err
			}
		}
		byPath := map[string][]int{}
		for _, h := range hunks {
			path, err := validatePath(h.Path)
			if err != nil {
				return err
			}
			if !full[path] {
				byPath[path] = append(byPath[path], h.Index)
			}
		}
		for path, indices := range byPath {
			names, err := git(ctx, root, "", nil, "diff", "--no-renames", "--name-only", "-z", base, expected.Tree, "--", path)
			if err != nil {
				return err
			}
			if string(names) != path+"\x00" {
				return fmt.Errorf("review_file_not_changed: %s", path)
			}
			patch, err := git(ctx, root, "", nil, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", "--full-index", "--unified=3", base, expected.Tree, "--", path)
			if err != nil {
				return err
			}
			subset, err := selectedHunks(string(patch), indices)
			if err != nil {
				return err
			}
			if _, err := git(ctx, root, index, []byte(subset), "apply", "--cached", "--check", "--binary", "-"); err != nil {
				return fmt.Errorf("review_patch_check_failed: %w", err)
			}
			if _, err := git(ctx, root, index, []byte(subset), "apply", "--cached", "--binary", "-"); err != nil {
				return err
			}
		}
	}
	target, err := textGit(ctx, root, index, "write-tree")
	if err != nil {
		return err
	}
	patch, err := git(ctx, root, "", nil, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", "--full-index", expected.Tree, target, "--")
	if err != nil {
		return err
	}
	if len(patch) > 0 {
		if _, err := git(ctx, root, "", patch, "apply", "--check", "--binary", "-"); err != nil {
			return fmt.Errorf("review_patch_check_failed: %w", err)
		}
	}
	// Keep the original reviewed tree reachable even after rejection and GC.
	// The recovery ref is local and never moves the user's branch.
	recovery, err := git(ctx, root, "", []byte("StudioForge review recovery\n"), "-c", "user.name=StudioForge", "-c", "user.email=noreply@studioforge.local", "commit-tree", expected.Tree, "-p", base)
	if err != nil {
		return err
	}
	if _, err := git(ctx, root, "", nil, "update-ref", "refs/studioforge/review-recovery/"+expected.Tree, strings.TrimSpace(string(recovery))); err != nil {
		return err
	}
	// Recheck just before mutation, after preparing the target and checkpoint.
	fresh, err := c.Diff(ctx, root, base)
	if err != nil {
		return err
	}
	if fresh.Tree != expected.Tree || fresh.Head != expected.Head || fresh.Index != expected.Index {
		return errors.New("review_tree_changed: files changed while preparing resolution; current files were preserved")
	}
	if len(patch) > 0 {
		if _, err := git(ctx, root, "", patch, "apply", "--binary", "-"); err != nil {
			return err
		}
	}
	if _, err := git(ctx, root, "", nil, "read-tree", target); err != nil {
		if len(patch) > 0 {
			// A cancelled request cannot cancel restoration of the pre-resolution tree.
			recoveryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, undoErr := git(recoveryCtx, root, "", patch, "apply", "--reverse", "--binary", "-")
			if undoErr != nil {
				return fmt.Errorf("review index update failed: %v; restore recovery ref %s: %w", err, expected.Tree, undoErr)
			}
		}
		return err
	}
	return nil
}

// Compatibility wrappers resolve an immediate snapshot. Long-lived decisions
// must persist Snapshot and call Resolve, so intervening edits are detected.
func (c *Client) Reject(ctx context.Context, root, base string) error {
	s, err := c.Diff(ctx, root, base)
	if err != nil {
		return err
	}
	return c.Resolve(ctx, root, base, s, "reject", nil, nil)
}
func (c *Client) ApplySelected(ctx context.Context, root, base string, selected, newFiles []string) error {
	for _, path := range selected {
		if _, err := validatePath(path); err != nil {
			return err
		}
	}
	s, err := c.Diff(ctx, root, base)
	if err != nil {
		return err
	}
	return c.Resolve(ctx, root, base, s, "apply_selected", selected, nil)
}

// HunkCount is the UI's per-file hunk count for a stable, single-file patch.
func HunkCount(patch string) int {
	n := 0
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "@@ ") {
			n++
		}
	}
	return n
}

// File is the stable selection model shown with a pending review. File names
// come from Git's NUL-delimited output; hunk text is never used to derive paths.
type File struct {
	Path  string   `json:"path"`
	Hunks []string `json:"hunks"`
}

func (c *Client) Files(ctx context.Context, root, base, tree string) ([]File, error) {
	names, err := git(ctx, root, "", nil, "diff", "--no-renames", "--name-only", "-z", base, tree, "--")
	if err != nil {
		return nil, err
	}
	files := []File{}
	for _, path := range strings.Split(string(names), "\x00") {
		if path == "" {
			continue
		}
		patch, err := git(ctx, root, "", nil, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", "--full-index", base, tree, "--", path)
		if err != nil {
			return nil, err
		}
		file := File{Path: path, Hunks: []string{}}
		text := string(patch)
		if !strings.Contains(text, "GIT binary patch") && !strings.Contains(text, "new file mode") && !strings.Contains(text, "deleted file mode") && !strings.Contains(text, "old mode") {
			for _, line := range strings.SplitAfter(text, "\n") {
				if strings.HasPrefix(line, "@@ ") {
					file.Hunks = append(file.Hunks, line)
				} else if len(file.Hunks) > 0 {
					file.Hunks[len(file.Hunks)-1] += line
				}
			}
		}
		files = append(files, file)
	}
	return files, nil
}
