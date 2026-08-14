export type TokenKind = 'keyword' | 'string' | 'comment' | 'number' | 'plain';
export type Token = { text: string; kind: TokenKind };

const LUAU_KEYWORDS = new Set([
  'and',
  'break',
  'continue',
  'do',
  'else',
  'elseif',
  'end',
  'false',
  'for',
  'function',
  'if',
  'in',
  'local',
  'nil',
  'not',
  'or',
  'repeat',
  'return',
  'then',
  'true',
  'until',
  'while',
  'type',
  'export',
]);

export function languageForPath(path: string): 'luau' | 'json' | 'markdown' | null {
  const match = /\.([a-z0-9]+)$/i.exec(path);
  if (!match) return null;
  const ext = match[1].toLowerCase();
  if (ext === 'lua' || ext === 'luau') return 'luau';
  if (ext === 'json') return 'json';
  if (ext === 'md') return 'markdown';
  return null;
}

export function tokenizeLine(line: string, lang: 'luau' | 'json' | 'markdown' | null): Token[] {
  if (line.length === 0) return [];
  if (lang === 'luau') return tokenizeLuau(line);
  if (lang === 'json') return tokenizeJson(line);
  if (lang === 'markdown') return tokenizeMarkdown(line);
  return [{ text: line, kind: 'plain' }];
}

function pushPlain(tokens: Token[], text: string): void {
  if (text.length === 0) return;
  const last = tokens[tokens.length - 1];
  if (last && last.kind === 'plain') {
    last.text += text;
  } else {
    tokens.push({ text, kind: 'plain' });
  }
}

function tokenizeLuau(line: string): Token[] {
  const tokens: Token[] = [];
  let i = 0;
  const n = line.length;
  while (i < n) {
    const ch = line[i];

    if (ch === '-' && line[i + 1] === '-') {
      tokens.push({ text: line.slice(i), kind: 'comment' });
      break;
    }

    if (ch === '"' || ch === "'") {
      const quote = ch;
      let j = i + 1;
      while (j < n) {
        if (line[j] === '\\' && j + 1 < n) {
          j += 2;
          continue;
        }
        if (line[j] === quote) {
          j += 1;
          break;
        }
        j += 1;
      }
      tokens.push({ text: line.slice(i, j), kind: 'string' });
      i = j;
      continue;
    }

    if (/[0-9]/.test(ch) || (ch === '.' && /[0-9]/.test(line[i + 1] ?? ''))) {
      const rest = line.slice(i);
      const numMatch = /^(0x[0-9a-fA-F]+|[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?)/.exec(rest);
      if (numMatch) {
        tokens.push({ text: numMatch[0], kind: 'number' });
        i += numMatch[0].length;
        continue;
      }
    }

    if (/[A-Za-z_]/.test(ch)) {
      const rest = line.slice(i);
      const wordMatch = /^[A-Za-z_][A-Za-z0-9_]*/.exec(rest);
      const word = wordMatch ? wordMatch[0] : ch;
      if (LUAU_KEYWORDS.has(word)) {
        tokens.push({ text: word, kind: 'keyword' });
      } else {
        pushPlain(tokens, word);
      }
      i += word.length;
      continue;
    }

    pushPlain(tokens, ch);
    i += 1;
  }
  return tokens;
}

function tokenizeJson(line: string): Token[] {
  const tokens: Token[] = [];
  let i = 0;
  const n = line.length;
  while (i < n) {
    const ch = line[i];

    if (ch === '"') {
      let j = i + 1;
      while (j < n) {
        if (line[j] === '\\' && j + 1 < n) {
          j += 2;
          continue;
        }
        if (line[j] === '"') {
          j += 1;
          break;
        }
        j += 1;
      }
      tokens.push({ text: line.slice(i, j), kind: 'string' });
      i = j;
      continue;
    }

    const rest = line.slice(i);
    const numMatch = /^-?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?/.exec(rest);
    if (numMatch && numMatch[0].length > 0) {
      tokens.push({ text: numMatch[0], kind: 'number' });
      i += numMatch[0].length;
      continue;
    }

    const litMatch = /^(true|false|null)/.exec(rest);
    if (litMatch) {
      tokens.push({ text: litMatch[0], kind: 'keyword' });
      i += litMatch[0].length;
      continue;
    }

    pushPlain(tokens, ch);
    i += 1;
  }
  return tokens;
}

function tokenizeMarkdown(line: string): Token[] {
  if (/^#{1,6}\s/.test(line) || /^#{1,6}$/.test(line)) {
    return [{ text: line, kind: 'keyword' }];
  }

  const tokens: Token[] = [];
  let i = 0;
  const n = line.length;
  while (i < n) {
    if (line[i] === '`') {
      let j = i + 1;
      while (j < n && line[j] !== '`') j += 1;
      if (j < n) {
        tokens.push({ text: line.slice(i, j + 1), kind: 'string' });
        i = j + 1;
        continue;
      }
    }

    if (line[i] === '*' && line[i + 1] === '*') {
      let j = i + 2;
      while (j < n - 1 && !(line[j] === '*' && line[j + 1] === '*')) j += 1;
      if (j < n - 1) {
        tokens.push({ text: line.slice(i, j + 2), kind: 'keyword' });
        i = j + 2;
        continue;
      }
    }

    pushPlain(tokens, line[i]);
    i += 1;
  }
  return tokens;
}
