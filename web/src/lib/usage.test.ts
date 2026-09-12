import { describe, expect, it } from 'vitest';
import { usageCSV, usageGroups, usageClass, type UsageRun } from './usage';
const run: UsageRun = {
  id: 'r',
  agentId: 'a',
  agentName: '=cmd()',
  provider: 'p',
  model: 'm',
  createdAt: '2026-09-13',
  cost: 1.5,
  recorded: true,
  inputTokens: 10,
  outputTokens: 20,
  cacheReadTokens: 3,
  cacheCreationTokens: 4,
};
describe('usage reports', () => {
  it('groups by stable agent ID and counts cache tokens once', () => {
    expect(usageGroups([run, { ...run, id: 'r2', agentId: 'b', cost: 2 }], 'agentId')).toHaveLength(
      2,
    );
    expect(usageGroups([run, { ...run, id: 'r2', cost: 2 }], 'provider')[0]).toMatchObject({
      cost: 3.5,
      tokens: 74,
      runs: 2,
    });
  });
  it('exports quoted CSV without formula injection and preserves missing accounting', () => {
    const csv = usageCSV([{ ...run, model: 'a,"b', recorded: false }]);
    expect(csv).toContain('"\'=cmd()"');
    expect(csv).toContain('"a,""b"');
    expect(csv).toContain('"false"');
    expect(csv).toContain('\r\n');
  });
});

it('does not mistake missing subscription costs for free inference', () => {
  expect(usageClass({ ...run, provider: 'claudecode', cost: 0, recorded: false })).toBe('unknown');
  expect(usageClass({ ...run, provider: 'openrouter', model: 'vendor/model:free', cost: 0 })).toBe(
    'free',
  );
  expect(usageClass({ ...run, cost: 0 })).toBe('zero');
});
