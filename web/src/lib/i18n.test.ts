import { describe, expect, it } from 'vitest';
import { en, formatDate, formatMoney, ru } from './i18n';

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
