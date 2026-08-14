package diffparse

import (
	"fmt"
	"strings"
)

// FormatFilePatch renders a single file's diff --git header plus the hunks
// selected by hunkIndexes back into unified-diff text, so a caller that
// picked a subset of hunks out of a parsed Diff can hand just that subset to
// `git apply`. hunkIndexes selects file.Hunks by position; nil or empty
// means every hunk. It never emits the "index"/"mode" lines Parse ignores on
// the way in, since nothing downstream needs them to apply a patch.
func FormatFilePatch(file DiffFile, hunkIndexes []int) (string, error) {
	if file.Binary {
		return "", fmt.Errorf("cannot format a patch for binary file %s", file.Path)
	}
	if len(file.Hunks) == 0 {
		return "", fmt.Errorf("file %s has no hunks to format", file.Path)
	}
	indexes := hunkIndexes
	if len(indexes) == 0 {
		indexes = make([]int, len(file.Hunks))
		for i := range file.Hunks {
			indexes[i] = i
		}
	}
	for _, idx := range indexes {
		if idx < 0 || idx >= len(file.Hunks) {
			return "", fmt.Errorf("hunk index %d out of range for file %s", idx, file.Path)
		}
	}

	oldPath := file.Path
	if file.OldPath != nil {
		oldPath = *file.OldPath
	}
	newPath := file.Path

	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", oldPath, newPath)
	switch file.Status {
	case StatusAdded:
		b.WriteString("--- /dev/null\n")
		fmt.Fprintf(&b, "+++ b/%s\n", newPath)
	case StatusDeleted:
		fmt.Fprintf(&b, "--- a/%s\n", oldPath)
		b.WriteString("+++ /dev/null\n")
	default:
		fmt.Fprintf(&b, "--- a/%s\n", oldPath)
		fmt.Fprintf(&b, "+++ b/%s\n", newPath)
	}

	for _, idx := range indexes {
		hunk := file.Hunks[idx]
		b.WriteString(hunk.Header)
		b.WriteString("\n")
		for _, line := range hunk.Lines {
			switch line.Type {
			case LineAdd:
				b.WriteString("+")
			case LineDelete:
				b.WriteString("-")
			default:
				b.WriteString(" ")
			}
			b.WriteString(line.Text)
			b.WriteString("\n")
			if line.NoNewline {
				b.WriteString("\\ No newline at end of file\n")
			}
		}
	}
	return b.String(), nil
}
