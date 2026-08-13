package diffparse

import "testing"

func intPtr(n int) *int { return &n }

func TestParseModifiedFile(t *testing.T) {
	text := `diff --git a/src/Main.luau b/src/Main.luau
index abc1234..def5678 100644
--- a/src/Main.luau
+++ b/src/Main.luau
@@ -1,3 +1,5 @@
 local Players = game:GetService("Players")
-local function noop() end
+local function onSpawn()
+	print("spawned")
+end
 return true`

	diff := Parse(text)
	if diff.Stats != (DiffStats{FilesChanged: 1, Additions: 3, Deletions: 1}) {
		t.Fatalf("stats=%+v", diff.Stats)
	}
	if len(diff.Files) != 1 {
		t.Fatalf("files=%d", len(diff.Files))
	}
	f := diff.Files[0]
	if f.Path != "src/Main.luau" || f.OldPath != nil || f.Status != StatusModified || f.Binary {
		t.Fatalf("file=%+v", f)
	}
	if f.Additions != 3 || f.Deletions != 1 {
		t.Fatalf("file counts=%+v", f)
	}
	if len(f.Hunks) != 1 {
		t.Fatalf("hunks=%d", len(f.Hunks))
	}
	h := f.Hunks[0]
	if h.Header != "@@ -1,3 +1,5 @@" || h.OldStart != 1 || h.OldLines != 3 || h.NewStart != 1 || h.NewLines != 5 {
		t.Fatalf("hunk=%+v", h)
	}
	wantLines := []DiffLine{
		{Type: LineContext, OldNo: intPtr(1), NewNo: intPtr(1), Text: `local Players = game:GetService("Players")`},
		{Type: LineDelete, OldNo: intPtr(2), Text: "local function noop() end"},
		{Type: LineAdd, NewNo: intPtr(2), Text: "local function onSpawn()"},
		{Type: LineAdd, NewNo: intPtr(3), Text: "\tprint(\"spawned\")"},
		{Type: LineAdd, NewNo: intPtr(4), Text: "end"},
		{Type: LineContext, OldNo: intPtr(3), NewNo: intPtr(5), Text: "return true"},
	}
	assertLines(t, h.Lines, wantLines)
}

func TestParseAddedFile(t *testing.T) {
	text := `diff --git a/src/New.luau b/src/New.luau
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/src/New.luau
@@ -0,0 +1,2 @@
+print("hello")
+return true`

	diff := Parse(text)
	if len(diff.Files) != 1 {
		t.Fatalf("files=%d", len(diff.Files))
	}
	f := diff.Files[0]
	if f.Status != StatusAdded || f.OldPath != nil || f.Additions != 2 || f.Deletions != 0 {
		t.Fatalf("file=%+v", f)
	}
	if diff.Stats.Additions != 2 || diff.Stats.Deletions != 0 || diff.Stats.FilesChanged != 1 {
		t.Fatalf("stats=%+v", diff.Stats)
	}
}

func TestParseDeletedFile(t *testing.T) {
	text := `diff --git a/src/Old.luau b/src/Old.luau
deleted file mode 100644
index abc1234..0000000
--- a/src/Old.luau
+++ /dev/null
@@ -1,2 +0,0 @@
-print("bye")
-return false`

	diff := Parse(text)
	if len(diff.Files) != 1 {
		t.Fatalf("files=%d", len(diff.Files))
	}
	f := diff.Files[0]
	if f.Status != StatusDeleted || f.Additions != 0 || f.Deletions != 2 {
		t.Fatalf("file=%+v", f)
	}
}

func TestParseRenamedFileWithContentChange(t *testing.T) {
	text := `diff --git a/src/Old.luau b/src/Renamed.luau
similarity index 88%
rename from src/Old.luau
rename to src/Renamed.luau
index abc1234..def5678 100644
--- a/src/Old.luau
+++ b/src/Renamed.luau
@@ -1,2 +1,2 @@
 local x = 1
-print(x)
+print(x + 1)`

	diff := Parse(text)
	if len(diff.Files) != 1 {
		t.Fatalf("files=%d", len(diff.Files))
	}
	f := diff.Files[0]
	if f.Status != StatusRenamed || f.OldPath == nil || *f.OldPath != "src/Old.luau" || f.Path != "src/Renamed.luau" {
		t.Fatalf("file=%+v", f)
	}
	if f.Additions != 1 || f.Deletions != 1 {
		t.Fatalf("counts=%+v", f)
	}
}

func TestParsePureRenameHasNoHunks(t *testing.T) {
	text := `diff --git a/src/A.luau b/src/B.luau
similarity index 100%
rename from src/A.luau
rename to src/B.luau`

	diff := Parse(text)
	f := diff.Files[0]
	if f.Status != StatusRenamed || f.OldPath == nil || *f.OldPath != "src/A.luau" || f.Path != "src/B.luau" {
		t.Fatalf("file=%+v", f)
	}
	if len(f.Hunks) != 0 || f.Additions != 0 || f.Deletions != 0 {
		t.Fatalf("expected no hunks/changes, got %+v", f)
	}
}

func TestParseCopiedFile(t *testing.T) {
	text := `diff --git a/src/Template.luau b/src/Copy.luau
similarity index 100%
copy from src/Template.luau
copy to src/Copy.luau`

	diff := Parse(text)
	f := diff.Files[0]
	if f.Status != StatusCopied || f.OldPath == nil || *f.OldPath != "src/Template.luau" || f.Path != "src/Copy.luau" {
		t.Fatalf("file=%+v", f)
	}
}

func TestParseModeChangeOnly(t *testing.T) {
	text := `diff --git a/scripts/run.sh b/scripts/run.sh
old mode 100644
new mode 100755`

	diff := Parse(text)
	f := diff.Files[0]
	if f.Status != StatusModified || f.Binary || len(f.Hunks) != 0 || f.Additions != 0 || f.Deletions != 0 {
		t.Fatalf("file=%+v", f)
	}
	if f.OldPath != nil {
		t.Fatalf("oldPath should be nil for a pure mode change, got %v", *f.OldPath)
	}
}

func TestParseBinaryModifiedFile(t *testing.T) {
	text := `diff --git a/assets/icon.png b/assets/icon.png
index abc1234..def5678 100644
Binary files a/assets/icon.png and b/assets/icon.png differ`

	diff := Parse(text)
	f := diff.Files[0]
	if !f.Binary || f.Status != StatusModified || len(f.Hunks) != 0 {
		t.Fatalf("file=%+v", f)
	}
}

func TestParseBinaryAddedFile(t *testing.T) {
	text := `diff --git a/assets/new.png b/assets/new.png
new file mode 100644
index 0000000..abc1234
Binary files /dev/null and b/assets/new.png differ`

	diff := Parse(text)
	f := diff.Files[0]
	if !f.Binary || f.Status != StatusAdded || len(f.Hunks) != 0 {
		t.Fatalf("file=%+v", f)
	}
}

func TestParseEmptyDiffReturnsEmptyNonNilFiles(t *testing.T) {
	for _, text := range []string{"", "   ", "\n\n"} {
		diff := Parse(text)
		if diff.Stats != (DiffStats{}) {
			t.Fatalf("stats=%+v for text=%q", diff.Stats, text)
		}
		if diff.Files == nil {
			t.Fatalf("files must not be nil for text=%q", text)
		}
		if len(diff.Files) != 0 {
			t.Fatalf("files=%d for text=%q", len(diff.Files), text)
		}
	}
}

func TestParseNoNewlineAtEndOfFileMarkerIsIgnored(t *testing.T) {
	text := `diff --git a/src/NoNewline.luau b/src/NoNewline.luau
index abc1234..def5678 100644
--- a/src/NoNewline.luau
+++ b/src/NoNewline.luau
@@ -1,1 +1,1 @@
-old text
\ No newline at end of file
+new text
\ No newline at end of file`

	diff := Parse(text)
	f := diff.Files[0]
	if f.Additions != 1 || f.Deletions != 1 {
		t.Fatalf("counts=%+v", f)
	}
	h := f.Hunks[0]
	if len(h.Lines) != 2 {
		t.Fatalf("no-newline markers must not become lines, got %d: %+v", len(h.Lines), h.Lines)
	}
	if h.Lines[0].Type != LineDelete || h.Lines[0].Text != "old text" {
		t.Fatalf("delete line=%+v", h.Lines[0])
	}
	if h.Lines[1].Type != LineAdd || h.Lines[1].Text != "new text" {
		t.Fatalf("add line=%+v", h.Lines[1])
	}
}

func TestParseMultipleFilesAggregatesStats(t *testing.T) {
	text := `diff --git a/a.luau b/a.luau
index 111..222 100644
--- a/a.luau
+++ b/a.luau
@@ -1,1 +1,1 @@
-old
+new
diff --git a/b.luau b/b.luau
new file mode 100644
index 0000000..333
--- /dev/null
+++ b/b.luau
@@ -0,0 +1,1 @@
+created`

	diff := Parse(text)
	if diff.Stats.FilesChanged != 2 || diff.Stats.Additions != 2 || diff.Stats.Deletions != 1 {
		t.Fatalf("stats=%+v", diff.Stats)
	}
}

func TestParseMalformedInputDoesNotPanic(t *testing.T) {
	texts := []string{
		"not a diff at all",
		"diff --git a/x b/x\n@@ garbage @@\n+still added",
		"@@ -1,1 +1,1 @@\n+orphan hunk with no file header",
	}
	for _, text := range texts {
		diff := Parse(text)
		if diff.Files == nil {
			t.Fatalf("files must not be nil for text=%q", text)
		}
	}
}

func assertLines(t *testing.T, got, want []DiffLine) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("line count got=%d want=%d\ngot=%+v\nwant=%+v", len(got), len(want), got, want)
	}
	for i := range got {
		g, w := got[i], want[i]
		if g.Type != w.Type || g.Text != w.Text || !intPtrEqual(g.OldNo, w.OldNo) || !intPtrEqual(g.NewNo, w.NewNo) {
			t.Fatalf("line[%d] got=%+v want=%+v", i, g, w)
		}
	}
}

func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
