import type { DiffLine } from '$lib/types';

export type Span = { start: number; end: number };

const MAX_CHARS = 1000;
const MAX_TOKENS = 300;
const MIN_SIMILARITY = 0.3;

export function pairChangedLines(lines: DiffLine[]): Array<{ deleteIdx: number; addIdx: number }> {
  const pairs: Array<{ deleteIdx: number; addIdx: number }> = [];
  let i = 0;
  const n = lines.length;
  while (i < n) {
    if (lines[i].type === 'delete') {
      const deleteStart = i;
      let j = i;
      while (j < n && lines[j].type === 'delete') j += 1;
      const deleteEnd = j;
      const addStart = j;
      let k = j;
      while (k < n && lines[k].type === 'add') k += 1;
      const addEnd = k;
      const count = Math.min(deleteEnd - deleteStart, addEnd - addStart);
      for (let p = 0; p < count; p += 1) {
        pairs.push({ deleteIdx: deleteStart + p, addIdx: addStart + p });
      }
      i = k;
    } else {
      i += 1;
    }
  }
  return pairs;
}

function tokenize(text: string): string[] {
  const matches = text.match(/(\w+|\s+|[^\w\s])/g);
  return matches ?? [];
}

function lcsMatches(a: string[], b: string[]): Array<[number, number]> {
  const n = a.length;
  const m = b.length;
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i -= 1) {
    for (let j = m - 1; j >= 0; j -= 1) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const matches: Array<[number, number]> = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      matches.push([i, j]);
      i += 1;
      j += 1;
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      i += 1;
    } else {
      j += 1;
    }
  }
  return matches;
}

function tokenOffsets(tokens: string[]): number[] {
  const offsets = [0];
  for (const token of tokens) {
    offsets.push(offsets[offsets.length - 1] + token.length);
  }
  return offsets;
}

function unmatchedSpans(tokens: string[], matched: Set<number>): Span[] {
  const offsets = tokenOffsets(tokens);
  const spans: Span[] = [];
  let i = 0;
  while (i < tokens.length) {
    if (matched.has(i)) {
      i += 1;
      continue;
    }
    let j = i;
    while (j < tokens.length && !matched.has(j)) j += 1;
    spans.push({ start: offsets[i], end: offsets[j] });
    i = j;
  }
  return spans;
}

export function intralineSpans(
  oldText: string,
  newText: string,
): { old: Span[]; neu: Span[] } | null {
  if (oldText.length > MAX_CHARS || newText.length > MAX_CHARS) return null;

  const oldTokens = tokenize(oldText);
  const newTokens = tokenize(newText);
  if (oldTokens.length > MAX_TOKENS || newTokens.length > MAX_TOKENS) return null;

  const totalLen = oldTokens.length + newTokens.length;
  if (totalLen === 0) return { old: [], neu: [] };

  const matches = lcsMatches(oldTokens, newTokens);
  const similarity = (2 * matches.length) / totalLen;
  if (similarity < MIN_SIMILARITY) return null;

  const matchedOld = new Set(matches.map(([i]) => i));
  const matchedNew = new Set(matches.map(([, j]) => j));

  return {
    old: unmatchedSpans(oldTokens, matchedOld),
    neu: unmatchedSpans(newTokens, matchedNew),
  };
}

export function applySpans(
  tokens: { text: string; kind: string }[],
  spans: Span[],
): Array<{ text: string; kind: string; emph: boolean }> {
  const result: Array<{ text: string; kind: string; emph: boolean }> = [];
  let pos = 0;
  let spanIdx = 0;

  for (const token of tokens) {
    const tokenStart = pos;
    const tokenEnd = pos + token.text.length;
    let cursor = tokenStart;

    while (cursor < tokenEnd) {
      while (spanIdx < spans.length && spans[spanIdx].end <= cursor) spanIdx += 1;
      const currentSpan = spans[spanIdx];
      const insideSpan = !!currentSpan && currentSpan.start <= cursor && cursor < currentSpan.end;
      let segEnd: number;
      if (insideSpan) {
        segEnd = Math.min(currentSpan.end, tokenEnd);
      } else if (currentSpan && currentSpan.start < tokenEnd) {
        segEnd = Math.min(currentSpan.start, tokenEnd);
      } else {
        segEnd = tokenEnd;
      }
      const text = token.text.slice(cursor - tokenStart, segEnd - tokenStart);
      if (text.length > 0) {
        result.push({ text, kind: token.kind, emph: insideSpan });
      }
      cursor = segEnd;
    }

    pos = tokenEnd;
  }

  return result;
}
