// Package diffparse turns a unified diff (the exact text `git diff` prints)
// into a typed model, so the API can hand a structured response to callers
// that need per-file and per-hunk navigation instead of one opaque string.
// It never shells out to git itself and never panics on malformed input —
// anything it cannot make sense of is skipped, so a partially garbled diff
// still yields whatever the parser could recover rather than an error.
package diffparse

import (
	"regexp"
	"strconv"
	"strings"
)

// Diff is the top-level structured model for a full `git diff` invocation.
type Diff struct {
	Stats DiffStats  `json:"stats"`
	Files []DiffFile `json:"files"`
}

// DiffStats summarizes a Diff across every file it touched.
type DiffStats struct {
	FilesChanged int `json:"filesChanged"`
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
}

// DiffFile is one file's entry in a Diff. OldPath is non-nil only for
// renamed and copied files.
type DiffFile struct {
	Path      string     `json:"path"`
	OldPath   *string    `json:"oldPath"`
	Status    string     `json:"status"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Binary    bool       `json:"binary"`
	Hunks     []DiffHunk `json:"hunks"`
}

// File status values Parse assigns to DiffFile.Status.
const (
	StatusModified = "modified"
	StatusAdded    = "added"
	StatusDeleted  = "deleted"
	StatusRenamed  = "renamed"
	StatusCopied   = "copied"
)

// DiffHunk is one `@@ ... @@` block within a file's diff.
type DiffHunk struct {
	Header   string     `json:"header"`
	OldStart int        `json:"oldStart"`
	OldLines int        `json:"oldLines"`
	NewStart int        `json:"newStart"`
	NewLines int        `json:"newLines"`
	Lines    []DiffLine `json:"lines"`
}

// DiffLine is one line within a hunk. OldNo/NewNo are nil where that side has
// no corresponding line number (an added line has no OldNo, a deleted line
// has no NewNo).
type DiffLine struct {
	Type  string `json:"type"`
	OldNo *int   `json:"oldNo"`
	NewNo *int   `json:"newNo"`
	Text  string `json:"text"`
}

// Line type values Parse assigns to DiffLine.Type.
const (
	LineContext = "context"
	LineAdd     = "add"
	LineDelete  = "delete"
)

var (
	diffGitHeaderRe = regexp.MustCompile(`^diff --git a/(.*) b/(.*)$`)
	hunkHeaderRe    = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
)

// Parse turns raw unified-diff text (as printed by `git diff`) into a Diff.
// It never returns an error: text it does not recognize is skipped line by
// line, so a truncated or unexpected diff still yields the files and hunks
// it could make sense of rather than nothing at all. An empty or
// whitespace-only text yields a Diff with zero stats and an empty (never
// nil) Files slice.
func Parse(text string) Diff {
	diff := Diff{Files: []DiffFile{}}
	if strings.TrimSpace(text) == "" {
		return diff
	}

	var current *DiffFile
	var hunk *DiffHunk
	var oldLineNo, newLineNo int

	flushHunk := func() {
		if current != nil && hunk != nil {
			current.Hunks = append(current.Hunks, *hunk)
			hunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if current != nil {
			diff.Files = append(diff.Files, *current)
			current = nil
		}
	}

	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			path := ""
			if m := diffGitHeaderRe.FindStringSubmatch(line); m != nil {
				path = m[2]
			}
			current = &DiffFile{Path: path, Status: StatusModified, Hunks: []DiffHunk{}}
		case current == nil:
			continue
		case strings.HasPrefix(line, "old mode "), strings.HasPrefix(line, "new mode "):
		case strings.HasPrefix(line, "new file mode "):
			current.Status = StatusAdded
		case strings.HasPrefix(line, "deleted file mode "):
			current.Status = StatusDeleted
		case strings.HasPrefix(line, "rename from "):
			old := strings.TrimPrefix(line, "rename from ")
			current.OldPath = &old
			current.Status = StatusRenamed
		case strings.HasPrefix(line, "rename to "):
			current.Path = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "copy from "):
			old := strings.TrimPrefix(line, "copy from ")
			current.OldPath = &old
			current.Status = StatusCopied
		case strings.HasPrefix(line, "copy to "):
			current.Path = strings.TrimPrefix(line, "copy to ")
		case strings.HasPrefix(line, "Binary files "), strings.HasPrefix(line, "GIT binary patch"):
			current.Binary = true
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
		case strings.HasPrefix(line, "index "):
		case strings.HasPrefix(line, "similarity index "), strings.HasPrefix(line, "dissimilarity index "):
		case strings.HasPrefix(line, "@@ "):
			flushHunk()
			m := hunkHeaderRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			oldStart := atoiOr(m[1], 0)
			oldLines := atoiOr(m[2], 1)
			newStart := atoiOr(m[3], 0)
			newLines := atoiOr(m[4], 1)
			hunk = &DiffHunk{Header: line, OldStart: oldStart, OldLines: oldLines, NewStart: newStart, NewLines: newLines, Lines: []DiffLine{}}
			oldLineNo, newLineNo = oldStart, newStart
		case strings.HasPrefix(line, "\\ No newline at end of file"):
		case hunk != nil && strings.HasPrefix(line, "+"):
			n := newLineNo
			hunk.Lines = append(hunk.Lines, DiffLine{Type: LineAdd, NewNo: &n, Text: strings.TrimPrefix(line, "+")})
			newLineNo++
			current.Additions++
		case hunk != nil && strings.HasPrefix(line, "-"):
			o := oldLineNo
			hunk.Lines = append(hunk.Lines, DiffLine{Type: LineDelete, OldNo: &o, Text: strings.TrimPrefix(line, "-")})
			oldLineNo++
			current.Deletions++
		case hunk != nil && strings.HasPrefix(line, " "):
			o, n := oldLineNo, newLineNo
			hunk.Lines = append(hunk.Lines, DiffLine{Type: LineContext, OldNo: &o, NewNo: &n, Text: strings.TrimPrefix(line, " ")})
			oldLineNo++
			newLineNo++
		}
	}
	flushFile()

	diff.Stats.FilesChanged = len(diff.Files)
	for _, f := range diff.Files {
		diff.Stats.Additions += f.Additions
		diff.Stats.Deletions += f.Deletions
	}
	return diff
}

func atoiOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
