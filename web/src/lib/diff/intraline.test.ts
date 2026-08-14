import { describe, expect, it } from 'vitest';
import type { DiffLine } from '$lib/types';
import { applySpans, intralineSpans, pairChangedLines, type Span } from './intraline';

function line(type: DiffLine['type'], text: string): DiffLine {
  return { type, oldNo: null, newNo: null, text };
}

function slice(text: string, spans: Span[]): string[] {
  return spans.map((s) => text.slice(s.start, s.end));
}

describe('pairChangedLines', () => {
  it('pairs a single delete/add run', () => {
    const lines = [
      line('context', 'a'),
      line('delete', 'b'),
      line('add', 'c'),
      line('context', 'd'),
    ];
    expect(pairChangedLines(lines)).toEqual([{ deleteIdx: 1, addIdx: 2 }]);
  });
  it('pairs two deletes with one add, leaving a leftover unpaired', () => {
    const lines = [
      line('delete', 'a'),
      line('delete', 'b'),
      line('add', 'c'),
      line('context', 'd'),
    ];
    expect(pairChangedLines(lines)).toEqual([{ deleteIdx: 0, addIdx: 2 }]);
  });
  it('returns no pairs when there is no adjoining add run', () => {
    const lines = [line('delete', 'a'), line('context', 'b'), line('add', 'c')];
    expect(pairChangedLines(lines)).toEqual([]);
  });
  it('pairs multiple independent runs', () => {
    const lines = [
      line('delete', 'a'),
      line('add', 'b'),
      line('context', 'x'),
      line('delete', 'c'),
      line('delete', 'd'),
      line('add', 'e'),
      line('add', 'f'),
    ];
    expect(pairChangedLines(lines)).toEqual([
      { deleteIdx: 0, addIdx: 1 },
      { deleteIdx: 3, addIdx: 5 },
      { deleteIdx: 4, addIdx: 6 },
    ]);
  });
});

describe('intralineSpans', () => {
  it('highlights only the changed word', () => {
    const oldText = 'local speed = 16';
    const newText = 'local speed = 24';
    const result = intralineSpans(oldText, newText);
    expect(result).not.toBeNull();
    expect(slice(oldText, result!.old)).toEqual(['16']);
    expect(slice(newText, result!.neu)).toEqual(['24']);
  });

  it('handles a common prefix and suffix, highlighting only the middle token', () => {
    const oldText = 'return "old" end';
    const newText = 'return "new" end';
    const result = intralineSpans(oldText, newText);
    expect(result).not.toBeNull();
    expect(slice(oldText, result!.old)).toEqual(['old']);
    expect(slice(newText, result!.neu)).toEqual(['new']);
  });

  it('detects a whitespace-only change', () => {
    const oldText = 'a b';
    const newText = 'a  b';
    const result = intralineSpans(oldText, newText);
    expect(result).not.toBeNull();
    expect(result!.old.length + result!.neu.length).toBeGreaterThan(0);
  });

  it('returns null for completely different lines', () => {
    const oldText = 'local abcdefgh = 111111';
    const newText = 'return 999999999999999999999999';
    expect(intralineSpans(oldText, newText)).toBeNull();
  });

  it('returns null when a line exceeds the char guard', () => {
    const oldText = 'a'.repeat(1001);
    const newText = 'a'.repeat(1001);
    expect(intralineSpans(oldText, newText)).toBeNull();
  });

  it('returns null when a line exceeds the token guard', () => {
    const oldText = Array.from({ length: 301 }, (_, i) => `w${i}`).join(' ');
    const newText = oldText;
    expect(intralineSpans(oldText, newText)).toBeNull();
  });
});

describe('applySpans', () => {
  it('splits a token at a mid-token span boundary and marks emph', () => {
    const tokens = [{ text: 'localSpeed', kind: 'plain' }];
    const spans: Span[] = [{ start: 5, end: 10 }];
    const result = applySpans(tokens, spans);
    expect(result.map((t) => t.text).join('')).toBe('localSpeed');
    expect(result).toEqual([
      { text: 'local', kind: 'plain', emph: false },
      { text: 'Speed', kind: 'plain', emph: true },
    ]);
  });

  it('preserves exact concatenation across multiple tokens and spans', () => {
    const tokens = [
      { text: 'local ', kind: 'keyword' },
      { text: 'speed', kind: 'plain' },
      { text: ' = ', kind: 'plain' },
      { text: '16', kind: 'number' },
    ];
    const fullText = tokens.map((t) => t.text).join('');
    const spans: Span[] = [{ start: fullText.indexOf('16'), end: fullText.indexOf('16') + 2 }];
    const result = applySpans(tokens, spans);
    expect(result.map((t) => t.text).join('')).toBe(fullText);
    expect(result.find((t) => t.text === '16')?.emph).toBe(true);
    expect(result.filter((t) => t.emph)).toHaveLength(1);
  });

  it('returns tokens unmodified when there are no spans', () => {
    const tokens = [{ text: 'abc', kind: 'plain' }];
    const result = applySpans(tokens, []);
    expect(result).toEqual([{ text: 'abc', kind: 'plain', emph: false }]);
  });
});
