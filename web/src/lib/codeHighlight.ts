function escapeHTML(value: string): string {
  return value.replace(
    /[&<>"']/g,
    (character) =>
      ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[character] ??
      character,
  );
}

export function languageForPath(path: string): 'luau' | 'json' | 'markdown' | 'plain' {
  const lower = path.toLowerCase();
  if (lower.endsWith('.lua') || lower.endsWith('.luau')) return 'luau';
  if (lower.endsWith('.json')) return 'json';
  if (lower.endsWith('.md') || lower.endsWith('.markdown')) return 'markdown';
  return 'plain';
}

// Small render-time highlighter shared by the file viewer and diff rows. It
// intentionally returns escaped HTML and only adds inert <span> elements;
// project content is never treated as markup.
export function highlightCode(
  source: string,
  language: ReturnType<typeof languageForPath>,
): string {
  if (language === 'plain') return escapeHTML(source);
  return source
    .split('\n')
    .map((line) => highlightLine(line, language))
    .join('\n');
}

function highlightLine(
  line: string,
  language: Exclude<ReturnType<typeof languageForPath>, 'plain'>,
): string {
  if (language === 'markdown') {
    const heading = line.match(/^(#{1,6})(\s+.*)$/);
    if (heading) return `<span class="syntax-heading">${escapeHTML(line)}</span>`;
  }
  const pattern =
    /(--.*|\/\/.*|#[^\n]*|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`(?:\\.|[^`\\])*`|\b(?:local|function|end|if|then|else|elseif|for|while|do|return|and|or|not|true|false|nil|require|module|export|in)\b)/g;
  let output = '';
  let last = 0;
  for (const match of line.matchAll(pattern)) {
    const index = match.index ?? 0;
    output += escapeHTML(line.slice(last, index));
    const token = match[0];
    const escaped = escapeHTML(token);
    const isComment =
      token.startsWith('--') ||
      token.startsWith('//') ||
      (language === 'markdown' && token.startsWith('#'));
    const isString = /^['"`]/.test(token);
    const isKeyword = language === 'luau' && !isComment && !isString;
    output += `<span class="${isComment ? 'syntax-comment' : isString ? 'syntax-string' : isKeyword ? 'syntax-keyword' : 'syntax-value'}">${escaped}</span>`;
    last = index + token.length;
  }
  return output + escapeHTML(line.slice(last));
}
