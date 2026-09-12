import type { TokenUsage } from './types';

export type UsageRun = TokenUsage & {
  id: string;
  agentId: string;
  agentName: string;
  provider: string;
  model: string;
  createdAt: string;
  cost: number;
  recorded: boolean;
};
export type UsageReport = {
  runs: UsageRun[];
  days: { key: string; cost: number }[];
  weeks: { key: string; cost: number }[];
  total: number;
  dailyLimit: number;
  last24Hours: number;
  typicalSeconds: number;
  paceSamples: number;
};
export function usageTokens(run: UsageRun): number {
  return run.inputTokens + run.outputTokens + run.cacheReadTokens + run.cacheCreationTokens;
}
export function usageGroups(runs: UsageRun[], field: 'agentId' | 'model' | 'provider') {
  const groups = new Map<
    string,
    { key: string; name: string; cost: number; tokens: number; runs: number }
  >();
  for (const run of runs) {
    const key = run[field];
    const group = groups.get(key) ?? {
      key,
      name: field === 'agentId' ? run.agentName : key,
      cost: 0,
      tokens: 0,
      runs: 0,
    };
    group.cost += run.cost;
    group.tokens += usageTokens(run);
    group.runs++;
    groups.set(key, group);
  }
  return [...groups.values()].sort((a, b) => b.cost - a.cost || a.name.localeCompare(b.name));
}
export function usageCSV(runs: UsageRun[]): string {
  // Spreadsheet formula prefixes are escaped even inside quoted cells.
  const cell = (value: string | number | boolean) => {
    let text = String(value);
    if (/^[\s]*[=+@-]/.test(text)) text = "'" + text;
    return '"' + text.replaceAll('"', '""') + '"';
  };
  const rows: (string | number | boolean)[][] = [
    [
      'run_id',
      'created_at',
      'agent',
      'provider',
      'model',
      'cost_usd',
      'usage_recorded',
      'input_tokens',
      'output_tokens',
      'cache_read_tokens',
      'cache_creation_tokens',
    ],
  ];
  for (const r of runs)
    rows.push([
      r.id,
      r.createdAt,
      r.agentName,
      r.provider,
      r.model,
      r.cost,
      r.recorded,
      r.inputTokens,
      r.outputTokens,
      r.cacheReadTokens,
      r.cacheCreationTokens,
    ]);
  return rows.map((row) => row.map(cell).join(',')).join('\r\n') + '\r\n';
}

// A zero charge alone is not proof of a free tier (CLI subscriptions often
// omit costs). Only explicit free-model identifiers classify as free.
export function usageClass(run: UsageRun): 'free' | 'paid' | 'zero' | 'unknown' {
  if (run.recorded && run.cost > 0) return 'paid';
  if (
    run.provider === 'openrouter' &&
    (run.model.endsWith(':free') || run.model === 'openrouter/free')
  )
    return 'free';
  return run.recorded ? 'zero' : 'unknown';
}
