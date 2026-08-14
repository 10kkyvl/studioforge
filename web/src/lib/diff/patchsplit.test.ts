import { describe, expect, it } from 'vitest';
import { splitPatchByFile } from './patchsplit';

const MULTI_FILE_DIFF = `diff --git a/src/foo.lua b/src/foo.lua
index 1111111..2222222 100644
--- a/src/foo.lua
+++ b/src/foo.lua
@@ -1,3 +1,3 @@
 local a = 1
-local b = 2
+local b = 3
 return a + b
diff --git a/src/bar.lua b/src/bar.lua
index 3333333..4444444 100644
--- a/src/bar.lua
+++ b/src/bar.lua
@@ -1,2 +1,2 @@
-local c = 4
+local c = 5
 return c
diff --git a/README.md b/README.md
index 5555555..6666666 100644
--- a/README.md
+++ b/README.md
@@ -1 +1 @@
-old text
+new text
`;

const RENAME_DIFF = `diff --git a/src/old.lua b/src/new.lua
similarity index 100%
rename from src/old.lua
rename to src/new.lua
`;

const BINARY_DIFF = `diff --git a/assets/icon.png b/assets/icon.png
index 7777777..8888888 100644
Binary files a/assets/icon.png and b/assets/icon.png differ
`;

const NO_NEWLINE_DIFF = `diff --git a/src/baz.lua b/src/baz.lua
index 9999999..aaaaaaa 100644
--- a/src/baz.lua
+++ b/src/baz.lua
@@ -1 +1 @@
-local z = 1
\\ No newline at end of file
+local z = 2
\\ No newline at end of file
`;

describe('splitPatchByFile', () => {
  it('splits a diff containing three files', () => {
    const result = splitPatchByFile(MULTI_FILE_DIFF);
    expect(result.map((r) => r.path)).toEqual(['src/foo.lua', 'src/bar.lua', 'README.md']);
    expect(result).toHaveLength(3);
    for (const entry of result) {
      expect(entry.patch.startsWith('diff --git ')).toBe(true);
    }
  });

  it('keys a rename entry by the new path', () => {
    const result = splitPatchByFile(RENAME_DIFF);
    expect(result).toHaveLength(1);
    expect(result[0].path).toBe('src/new.lua');
    expect(result[0].patch).toContain('rename from src/old.lua');
    expect(result[0].patch).toContain('rename to src/new.lua');
  });

  it('handles a binary file entry', () => {
    const result = splitPatchByFile(BINARY_DIFF);
    expect(result).toHaveLength(1);
    expect(result[0].path).toBe('assets/icon.png');
    expect(result[0].patch).toContain(
      'Binary files a/assets/icon.png and b/assets/icon.png differ',
    );
  });

  it('retains the trailing no-newline marker', () => {
    const result = splitPatchByFile(NO_NEWLINE_DIFF);
    expect(result).toHaveLength(1);
    expect(result[0].patch).toContain('\\ No newline at end of file');
  });

  it('returns an empty array for an empty string', () => {
    expect(splitPatchByFile('')).toEqual([]);
  });

  it('concatenates back to the original raw diff', () => {
    for (const raw of [MULTI_FILE_DIFF, RENAME_DIFF, BINARY_DIFF, NO_NEWLINE_DIFF]) {
      const result = splitPatchByFile(raw);
      expect(result.map((r) => r.patch).join('')).toBe(raw);
    }
  });
});
