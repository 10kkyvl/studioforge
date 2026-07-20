import { describe, expect, it } from 'vitest';
import {
  cacheTokens,
  en,
  formatDate,
  formatMoney,
  formatTokens,
  ru,
  spendTokens,
  totalTokens,
} from './i18n';

describe('i18n catalogs', () => {
  it('have exact key parity', () => expect(Object.keys(ru).sort()).toEqual(Object.keys(en).sort()));
  it('contain no empty translations', () => {
    for (const value of [...Object.values(en), ...Object.values(ru)])
      expect(value.trim()).not.toBe('');
  });
  it('format locale-aware values', () => {
    expect(formatMoney(12.5, 'en')).toContain('12.50');
    expect(formatDate('2026-01-01T12:00:00Z', 'ru')).not.toBe('');
  });
});

describe('token usage', () => {
  // Claude counts cache hits outside inputTokens, so a run that read 40k from
  // cache must not be reported as the 12 tokens it sent fresh.
  it('sums every counter', () => {
    expect(
      totalTokens({
        inputTokens: 12,
        outputTokens: 34,
        cacheReadTokens: 40_000,
        cacheCreationTokens: 100,
      }),
    ).toBe(40_146);
  });
  it('treats missing counters and absent usage as zero', () => {
    expect(totalTokens({ outputTokens: 5 })).toBe(5);
    expect(totalTokens(undefined)).toBe(0);
    expect(totalTokens(null)).toBe(0);
  });
  it('shortens large counts', () => {
    expect(formatTokens(12_400, 'en')).toBe('12.4K');
    expect(formatTokens(950, 'en')).toBe('950');
    expect(formatTokens(2_400_000, 'en')).toBe('2.4M');
  });
  // spendTokens is the headline: only the counters a budget actually tracks.
  it('spend sums input and output only', () => {
    expect(
      spendTokens({
        inputTokens: 12,
        outputTokens: 34,
        cacheReadTokens: 40_000,
        cacheCreationTokens: 100,
      }),
    ).toBe(46);
  });
  // cacheTokens is the quieter figure: context reused, not sent fresh.
  it('cache sums read and creation only', () => {
    expect(
      cacheTokens({
        inputTokens: 12,
        outputTokens: 34,
        cacheReadTokens: 40_000,
        cacheCreationTokens: 100,
      }),
    ).toBe(40_100);
  });
  it('spend and cache treat missing counters and absent usage as zero', () => {
    expect(spendTokens({ cacheReadTokens: 900 })).toBe(0);
    expect(cacheTokens({ inputTokens: 900 })).toBe(0);
    expect(spendTokens(undefined)).toBe(0);
    expect(spendTokens(null)).toBe(0);
    expect(cacheTokens(undefined)).toBe(0);
    expect(cacheTokens(null)).toBe(0);
  });
});
