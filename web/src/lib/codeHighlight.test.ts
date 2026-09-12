import { describe, expect, it } from 'vitest';
import { highlightCode, languageForPath } from './codeHighlight';

describe('code highlighting', () => {
  it.each(['example.luau', 'example.json', 'README.md', 'unknown.txt'])(
    'preserves file contents and escapes executable markup in %s',
    (path) => {
      const source =
        'local x = "<img src=x onerror=alert(1)>"\n-- <script>alert(1)</script>\n<&>\n';
      const container = document.createElement('div');
      container.innerHTML = highlightCode(source, languageForPath(path));
      expect(container.textContent).toBe(source);
      expect(container.querySelector('script, img, iframe')).toBeNull();
      expect([...container.querySelectorAll('*')].every((node) => node.tagName === 'SPAN')).toBe(
        true,
      );
    },
  );

  it('recognizes Luau without interpreting strings as comments', () => {
    const container = document.createElement('div');
    container.innerHTML = highlightCode('local url = "https://example.com" -- comment', 'luau');
    expect(container.querySelector('.syntax-keyword')?.textContent).toBe('local');
    expect(container.querySelector('.syntax-string')?.textContent).toBe('"https://example.com"');
    expect(container.querySelector('.syntax-comment')?.textContent).toBe('-- comment');
  });
});
