package gitops

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/10kkyvl/studioforge/internal/gitcheckpoint"
	"github.com/10kkyvl/studioforge/internal/gitops/diffparse"
)

var (
	ErrNotGitRepo       = errors.New("not a git repository")
	ErrDirtyWorktree    = errors.New("worktree has unresolved merge conflicts")
	ErrLaterChanges     = errors.New("a later checkpoint changed the selected paths")
	ErrPatchCheckFailed = errors.New("selected hunks no longer apply cleanly")
	ErrSelectionUnknown = errors.New("selection does not match the run's diff")
)

type HunkSelection struct {
	Path  string
	Index int
}

type SelectiveRollbackResult struct {
	SafetyCommit  string
	RevertedFiles int
	RevertedHunks int
}

func (c *Client) SelectiveRollback(ctx context.Context, root, checkpoint, nextCheckpoint string, files []string, hunks []HunkSelection) (SelectiveRollbackResult, error) {
	if err := validateRef(checkpoint); err != nil {
		return SelectiveRollbackResult{}, err
	}
	if nextCheckpoint != "" {
		if err := validateRef(nextCheckpoint); err != nil {
			return SelectiveRollbackResult{}, err
		}
	}
	if _, err := c.run(ctx, root, "rev-parse", "--git-dir"); err != nil {
		return SelectiveRollbackResult{}, ErrNotGitRepo
	}
	if _, err := c.run(ctx, root, "cat-file", "-e", checkpoint+"^{commit}"); err != nil {
		return SelectiveRollbackResult{}, fmt.Errorf("%w: %s", ErrUnknownRef, checkpoint)
	}
	conflicted, err := c.run(ctx, root, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return SelectiveRollbackResult{}, err
	}
	if conflicted != "" {
		return SelectiveRollbackResult{}, ErrDirtyWorktree
	}

	var laterPaths map[string]bool
	if nextCheckpoint != "" {
		laterOut, err := c.run(ctx, root, "diff", "--name-only", nextCheckpoint)
		if err != nil {
			return SelectiveRollbackResult{}, err
		}
		laterPaths = map[string]bool{}
		for _, p := range strings.Split(laterOut, "\n") {
			if p != "" {
				laterPaths[p] = true
			}
		}
	}

	var rawDiff string
	if nextCheckpoint != "" {
		rawDiff, err = c.DiffRange(ctx, root, checkpoint, nextCheckpoint)
	} else {
		rawDiff, err = c.DiffCommit(ctx, root, checkpoint)
	}
	if err != nil {
		return SelectiveRollbackResult{}, err
	}
	parsed := diffparse.Parse(rawDiff)

	filesByPath := make(map[string]diffparse.DiffFile, len(parsed.Files))
	for _, f := range parsed.Files {
		filesByPath[f.Path] = f
	}

	fileSelected := make(map[string]bool, len(files))
	for _, path := range files {
		fileSelected[path] = true
	}
	hunksByPath := make(map[string][]int)
	for _, h := range hunks {
		if fileSelected[h.Path] {
			return SelectiveRollbackResult{}, fmt.Errorf("%w: %s is selected both as a full file and by hunk", ErrSelectionUnknown, h.Path)
		}
		hunksByPath[h.Path] = append(hunksByPath[h.Path], h.Index)
	}

	for _, path := range files {
		f, ok := filesByPath[path]
		if !ok {
			return SelectiveRollbackResult{}, fmt.Errorf("%w: %s", ErrSelectionUnknown, path)
		}
		if f.Binary {
			return SelectiveRollbackResult{}, fmt.Errorf("%w: %s is a binary file", ErrSelectionUnknown, path)
		}
	}

	hunkPaths := make([]string, 0, len(hunksByPath))
	for path := range hunksByPath {
		hunkPaths = append(hunkPaths, path)
	}
	sort.Strings(hunkPaths)
	for _, path := range hunkPaths {
		f, ok := filesByPath[path]
		if !ok {
			return SelectiveRollbackResult{}, fmt.Errorf("%w: %s", ErrSelectionUnknown, path)
		}
		if f.Binary {
			return SelectiveRollbackResult{}, fmt.Errorf("%w: %s is a binary file", ErrSelectionUnknown, path)
		}
		for _, idx := range hunksByPath[path] {
			if idx < 0 || idx >= len(f.Hunks) {
				return SelectiveRollbackResult{}, fmt.Errorf("%w: hunk index %d out of range for %s", ErrSelectionUnknown, idx, path)
			}
		}
	}

	if laterPaths != nil {
		conflictSeen := map[string]bool{}
		var conflicting []string
		addConflict := func(path string) {
			if laterPaths[path] && !conflictSeen[path] {
				conflictSeen[path] = true
				conflicting = append(conflicting, path)
			}
		}
		for _, path := range files {
			addConflict(path)
			if f := filesByPath[path]; f.OldPath != nil {
				addConflict(*f.OldPath)
			}
		}
		for _, path := range hunkPaths {
			addConflict(path)
			if f := filesByPath[path]; f.OldPath != nil {
				addConflict(*f.OldPath)
			}
		}
		if len(conflicting) > 0 {
			sort.Strings(conflicting)
			return SelectiveRollbackResult{}, fmt.Errorf("%w: %s", ErrLaterChanges, strings.Join(conflicting, ", "))
		}
	}

	var patchParts []string
	for _, path := range hunkPaths {
		idxs := append([]int(nil), hunksByPath[path]...)
		sort.Ints(idxs)
		part, err := diffparse.FormatFilePatch(filesByPath[path], idxs)
		if err != nil {
			return SelectiveRollbackResult{}, fmt.Errorf("%w: %s: %v", ErrSelectionUnknown, path, err)
		}
		patchParts = append(patchParts, part)
	}
	patch := strings.Join(patchParts, "")
	if patch != "" {
		if _, err := c.runStdin(ctx, root, patch, "apply", "--check", "-R", "-"); err != nil {
			return SelectiveRollbackResult{}, fmt.Errorf("%w: %v", ErrPatchCheckFailed, err)
		}
	}

	safetyHash, _, err := gitcheckpoint.Checkpoint(root, "StudioForge checkpoint before selective rollback")
	if err != nil {
		return SelectiveRollbackResult{}, fmt.Errorf("safety checkpoint failed: %w", err)
	}

	for _, path := range files {
		f := filesByPath[path]
		switch f.Status {
		case diffparse.StatusAdded:
			if _, err := c.run(ctx, root, "rm", "-f", "--ignore-unmatch", "--", path); err != nil {
				return SelectiveRollbackResult{}, err
			}
		case diffparse.StatusRenamed:
			if _, err := c.run(ctx, root, "checkout", checkpoint, "--", *f.OldPath); err != nil {
				return SelectiveRollbackResult{}, err
			}
			if _, err := c.run(ctx, root, "rm", "-f", "--", path); err != nil {
				return SelectiveRollbackResult{}, err
			}
		default:
			if _, err := c.run(ctx, root, "checkout", checkpoint, "--", path); err != nil {
				return SelectiveRollbackResult{}, err
			}
		}
	}

	if patch != "" {
		if _, err := c.runStdin(ctx, root, patch, "apply", "-R", "-"); err != nil {
			return SelectiveRollbackResult{}, err
		}
	}

	return SelectiveRollbackResult{
		SafetyCommit:  safetyHash,
		RevertedFiles: len(files) + len(hunkPaths),
		RevertedHunks: len(hunks),
	}, nil
}
