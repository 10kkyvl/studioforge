import { describe, expect, it } from 'vitest';
import { languageForPath, tokenizeLine, type Token } from './highlight';

function joined(tokens: Token[]): string {
  return tokens.map((t) => t.text).join('');
}

describe('languageForPath', () => {
  it('maps lua and luau extensions to luau', () => {
    expect(languageForPath('src/Foo.lua')).toBe('luau');
    expect(languageForPath('src/Foo.luau')).toBe('luau');
  });
  it('maps json extension to json', () => {
    expect(languageForPath('config.json')).toBe('json');
  });
  it('maps md extension to markdown', () => {
    expect(languageForPath('README.md')).toBe('markdown');
  });
  it('is case-insensitive', () => {
    expect(languageForPath('Foo.LUAU')).toBe('luau');
    expect(languageForPath('Foo.JSON')).toBe('json');
    expect(languageForPath('Foo.MD')).toBe('markdown');
  });
  it('returns null for unknown extensions', () => {
    expect(languageForPath('Foo.txt')).toBeNull();
    expect(languageForPath('Foo')).toBeNull();
  });
});

describe('tokenizeLine luau', () => {
  it('distinguishes a comment from a minus operator', () => {
    const tokens = tokenizeLine('local x = a - b -- comment', 'luau');
    expect(tokens.find((t) => t.kind === 'comment')?.text).toBe('-- comment');
    expect(joined(tokens)).toBe('local x = a - b -- comment');
  });
  it('does not treat -- inside a string as a comment', () => {
    const tokens = tokenizeLine('local s = "a -- b"', 'luau');
    expect(tokens.some((t) => t.kind === 'comment')).toBe(false);
    expect(tokens.find((t) => t.kind === 'string')?.text).toBe('"a -- b"');
  });
  it('handles escaped quotes inside strings', () => {
    const tokens = tokenizeLine('local s = "a\\"b"', 'luau');
    const str = tokens.find((t) => t.kind === 'string');
    expect(str?.text).toBe('"a\\"b"');
    expect(joined(tokens)).toBe('local s = "a\\"b"');
  });
  it('recognizes hex and exponent numbers', () => {
    const tokens = tokenizeLine('local a = 0x1F local b = 1e5', 'luau');
    const numbers = tokens.filter((t) => t.kind === 'number').map((t) => t.text);
    expect(numbers).toEqual(['0x1F', '1e5']);
  });
  it('recognizes keywords', () => {
    const tokens = tokenizeLine('local speed = 16', 'luau');
    expect(tokens.find((t) => t.text === 'local')?.kind).toBe('keyword');
    expect(tokens.find((t) => t.text === '16')?.kind).toBe('number');
  });
  it('treats a bare minus as plain', () => {
    const tokens = tokenizeLine('a - b', 'luau');
    expect(tokens.some((t) => t.kind === 'comment')).toBe(false);
  });
});

describe('tokenizeLine json', () => {
  it('distinguishes keys and values as strings, and recognizes numbers', () => {
    const tokens = tokenizeLine('{"a": "b", "n": 42}', 'json');
    const strings = tokens.filter((t) => t.kind === 'string').map((t) => t.text);
    expect(strings).toEqual(['"a"', '"b"', '"n"']);
    expect(tokens.find((t) => t.kind === 'number')?.text).toBe('42');
  });
  it('recognizes true, false and null as keywords', () => {
    const tokens = tokenizeLine('{"a": true, "b": false, "c": null}', 'json');
    const keywords = tokens.filter((t) => t.kind === 'keyword').map((t) => t.text);
    expect(keywords).toEqual(['true', 'false', 'null']);
  });
});

describe('tokenizeLine markdown', () => {
  it('marks a heading line entirely as keyword', () => {
    const tokens = tokenizeLine('## Title here', 'markdown');
    expect(tokens).toEqual([{ text: '## Title here', kind: 'keyword' }]);
  });
  it('marks bold spans as keyword', () => {
    const tokens = tokenizeLine('this is **bold** text', 'markdown');
    expect(tokens.find((t) => t.kind === 'keyword')?.text).toBe('**bold**');
  });
  it('marks inline code spans as string', () => {
    const tokens = tokenizeLine('run `npm test` now', 'markdown');
    expect(tokens.find((t) => t.kind === 'string')?.text).toBe('`npm test`');
  });
});

describe('tokenizeLine edge cases', () => {
  it('returns an empty array for an empty line', () => {
    expect(tokenizeLine('', 'luau')).toEqual([]);
    expect(tokenizeLine('', null)).toEqual([]);
  });
  it('returns a single plain token for an unknown language', () => {
    expect(tokenizeLine('anything at all', null)).toEqual([
      { text: 'anything at all', kind: 'plain' },
    ]);
  });
});

describe('tokenizeLine exact concatenation property', () => {
  const cases: Array<[string, 'luau' | 'json' | 'markdown' | null]> = [
    ['local speed = 16 -- initial speed', 'luau'],
    ['local s = "a -- b" .. "c\\"d"', 'luau'],
    ['local hex = 0x1F + 1e5', 'luau'],
    ['', 'luau'],
    ['{"a": "b", "n": 42, "t": true}', 'json'],
    ['## Heading with **bold** and `code`', 'markdown'],
    ['plain markdown text', 'markdown'],
    ['arbitrary text', null],
  ];
  for (const [line, lang] of cases) {
    it(`round-trips ${JSON.stringify(line)} (${lang})`, () => {
      expect(joined(tokenizeLine(line, lang))).toBe(line);
    });
  }
});
