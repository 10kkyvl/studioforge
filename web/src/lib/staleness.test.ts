import { describe, expect, it } from 'vitest';
import { isStaleGeneration } from './staleness';

describe('isStaleGeneration', () => {
  it('is not stale when the captured generation still matches the current one', () => {
    expect(isStaleGeneration(3, 3)).toBe(false);
  });
  it('is stale once the current generation has moved on', () => {
    expect(isStaleGeneration(3, 4)).toBe(true);
    expect(isStaleGeneration(0, 1)).toBe(true);
  });
});
