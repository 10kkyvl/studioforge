package diffparse

import (
	"strings"
	"testing"
)

func TestFormatFilePatchRoundTripsMultiHunkModifiedFile(t *testing.T) {
	text := `diff --git a/src/A.luau b/src/A.luau
--- a/src/A.luau
+++ b/src/A.luau
@@ -1,3 +1,3 @@
 local x = 1
-local y = 2
+local y = 3
 local z = 4
@@ -10,2 +10,2 @@
-old10
+new10
 unchanged11`

	diff := Parse(text)
	if len(diff.Files) != 1 || len(diff.Files[0].Hunks) != 2 {
		t.Fatalf("files=%+v", diff.Files)
	}
	got, err := FormatFilePatch(diff.Files[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != text+"\n" {
		t.Fatalf("got=%q\nwant=%q", got, text+"\n")
	}
}

func TestFormatFilePatchSelectsSubsetOfHunks(t *testing.T) {
	text := `diff --git a/src/A.luau b/src/A.luau
--- a/src/A.luau
+++ b/src/A.luau
@@ -1,3 +1,3 @@
 local x = 1
-local y = 2
+local y = 3
 local z = 4
@@ -10,2 +10,2 @@
-old10
+new10
 unchanged11`

	diff := Parse(text)
	file := diff.Files[0]
	got, err := FormatFilePatch(file, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "local y") {
		t.Fatalf("first hunk must not be present, got=%q", got)
	}
	want := `diff --git a/src/A.luau b/src/A.luau
--- a/src/A.luau
+++ b/src/A.luau
@@ -10,2 +10,2 @@
-old10
+new10
 unchanged11`
	if got != want+"\n" {
		t.Fatalf("got=%q\nwant=%q", got, want+"\n")
	}
}

func TestFormatFilePatchNoNewlineRoundTrips(t *testing.T) {
	text := `diff --git a/src/N.luau b/src/N.luau
--- a/src/N.luau
+++ b/src/N.luau
@@ -1,1 +1,1 @@
-old text
\ No newline at end of file
+new text
\ No newline at end of file`

	diff := Parse(text)
	got, err := FormatFilePatch(diff.Files[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != text+"\n" {
		t.Fatalf("got=%q\nwant=%q", got, text+"\n")
	}
}

func TestFormatFilePatchAddedFileHeader(t *testing.T) {
	text := `diff --git a/src/New.luau b/src/New.luau
new file mode 100644
--- /dev/null
+++ b/src/New.luau
@@ -0,0 +1,2 @@
+print("hello")
+return true`

	diff := Parse(text)
	f := diff.Files[0]
	if f.Status != StatusAdded {
		t.Fatalf("status=%s", f.Status)
	}
	got, err := FormatFilePatch(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "diff --git a/src/New.luau b/src/New.luau\n--- /dev/null\n+++ b/src/New.luau\n") {
		t.Fatalf("got=%q", got)
	}
	if !strings.Contains(got, `+print("hello")`) || !strings.Contains(got, "+return true") {
		t.Fatalf("got=%q", got)
	}
}

func TestFormatFilePatchDeletedFileHeader(t *testing.T) {
	text := `diff --git a/src/Old.luau b/src/Old.luau
deleted file mode 100644
--- a/src/Old.luau
+++ /dev/null
@@ -1,2 +0,0 @@
-print("bye")
-return false`

	diff := Parse(text)
	f := diff.Files[0]
	if f.Status != StatusDeleted {
		t.Fatalf("status=%s", f.Status)
	}
	got, err := FormatFilePatch(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "diff --git a/src/Old.luau b/src/Old.luau\n--- a/src/Old.luau\n+++ /dev/null\n") {
		t.Fatalf("got=%q", got)
	}
	if !strings.Contains(got, `-print("bye")`) || !strings.Contains(got, "-return false") {
		t.Fatalf("got=%q", got)
	}
}

func TestFormatFilePatchRenamedFileUsesOldPathForAHeader(t *testing.T) {
	text := `diff --git a/src/Old.luau b/src/Renamed.luau
rename from src/Old.luau
rename to src/Renamed.luau
--- a/src/Old.luau
+++ b/src/Renamed.luau
@@ -1,2 +1,2 @@
 local x = 1
-print(x)
+print(x + 1)`

	diff := Parse(text)
	f := diff.Files[0]
	got, err := FormatFilePatch(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `diff --git a/src/Old.luau b/src/Renamed.luau
--- a/src/Old.luau
+++ b/src/Renamed.luau
@@ -1,2 +1,2 @@
 local x = 1
-print(x)
+print(x + 1)`
	if got != want+"\n" {
		t.Fatalf("got=%q\nwant=%q", got, want+"\n")
	}
}

func TestFormatFilePatchRejectsBinaryFile(t *testing.T) {
	f := DiffFile{Path: "assets/icon.png", Status: StatusModified, Binary: true, Hunks: []DiffHunk{{Header: "@@ -1 +1 @@"}}}
	if _, err := FormatFilePatch(f, nil); err == nil {
		t.Fatal("expected an error for a binary file")
	}
}

func TestFormatFilePatchRejectsFileWithNoHunks(t *testing.T) {
	f := DiffFile{Path: "src/A.luau", Status: StatusModified}
	if _, err := FormatFilePatch(f, nil); err == nil {
		t.Fatal("expected an error for a file with no hunks")
	}
}

func TestFormatFilePatchRejectsOutOfRangeHunkIndex(t *testing.T) {
	f := DiffFile{Path: "src/A.luau", Status: StatusModified, Hunks: []DiffHunk{{Header: "@@ -1 +1 @@"}}}
	if _, err := FormatFilePatch(f, []int{1}); err == nil {
		t.Fatal("expected an error for an out-of-range hunk index")
	}
}
