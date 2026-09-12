import { describe, expect, it } from 'vitest';
import { diffFiles } from './diffFiles';

describe('diff file links', () => {
  it('preserves the complete diff and gives one destination per file', () => {
    const diff =
      'diff --git a/src/main.lua b/src/main.lua\n--- a/src/main.lua\n+++ b/src/main.lua\n@@ -1 +1 @@\n-old\n+new\n';
    expect(diffFiles(diff)).toEqual([{ path: 'src/main.lua', deleted: false, text: diff }]);
  });
  it('handles spaces and ignores +++ inside a hunk', () => {
    const diff =
      'diff --git a/my file.lua b/my file.lua\n--- a/my file.lua\n+++ b/my file.lua\n@@ -0,0 +1 @@\n+++ not-a-path';
    expect(diffFiles(diff)[0].path).toBe('my file.lua');
  });
  it('decodes Git octal UTF-8 quoting for Cyrillic filenames', () => {
    const path = String.raw`"b/\321\202\320\265\321\201\321\202.lua"`;
    const diff = `diff --git "a/test.lua" ${path}\n--- /dev/null\n+++ ${path}\n@@ -0,0 +1 @@\n+return true`;
    expect(diffFiles(diff)[0].path).toBe('тест.lua');
  });
  it('does not offer a current-file link for deleted files', () => {
    const diff =
      'diff --git a/old.lua b/old.lua\ndeleted file mode 100755\n--- a/old.lua\n+++ /dev/null\n@@ -1 +0,0 @@\n-old';
    expect(diffFiles(diff)[0]).toMatchObject({ path: 'old.lua', deleted: true });
  });
  it('supports binary diffs and does not invent paths for non-diff text', () => {
    expect(diffFiles('diff --git a/image.png b/image.png\nBinary files differ')[0].path).toBe(
      'image.png',
    );
    expect(diffFiles('No changes')[0].path).toBe('');
  });
});
