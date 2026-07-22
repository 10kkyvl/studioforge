// Model suggestions for the agent editor.
//
// The value here is passed straight through to the CLI as `--model` (see
// internal/providers/claudecode/claude.go), so these are real model
// identifiers, not StudioForge-side tiers. That is also why the field stays
// free-text: a model released after this build must remain usable without
// waiting for StudioForge to ship a new list.
//
// Empty or "default" means "don't pass --model at all" and let the CLI choose.

export type ModelSuggestion = {
  id: string;
  label: string;
};

// Anthropic model IDs are exact strings — no date suffixes. A wrong ID reaches
// the CLI verbatim and fails at request time, so these are not guessed.
const CLAUDE_MODELS: ModelSuggestion[] = [
  { id: 'claude-opus-4-8', label: 'Opus 4.8 — most capable' },
  { id: 'claude-opus-4-7', label: 'Opus 4.7' },
  { id: 'claude-sonnet-5', label: 'Sonnet 5 — balanced' },
  { id: 'claude-sonnet-4-6', label: 'Sonnet 4.6' },
  { id: 'claude-haiku-4-5', label: 'Haiku 4.5 — fastest' },
];

export function modelsFor(provider: string): ModelSuggestion[] {
  if (provider === 'claude') return CLAUDE_MODELS;
  return [];
}

export const ACTIVE_PROVIDERS = ['claude', 'openrouter', 'mock'];

export function isLegacyProvider(provider: string): boolean {
  return !ACTIVE_PROVIDERS.includes(provider);
}
