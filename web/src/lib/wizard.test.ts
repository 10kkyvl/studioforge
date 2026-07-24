import { describe, expect, it } from 'vitest';
import type { Check, Diagnostics } from './types';
import { buildWizardChecks, completionTarget, deriveGates } from './wizard';

function check(patch: Partial<Check> = {}): Check {
  return { name: 'check', status: 'ok', ...patch };
}

function diagnostics(overrides: Partial<Diagnostics> = {}): Diagnostics {
  return {
    version: '1.0.0',
    os: 'windows',
    arch: 'amd64',
    dataPath: 'C:/data',
    database: 'ok',
    wal: true,
    fts5: true,
    safeMode: false,
    mockMode: false,
    dependencies: {
      claude: check({ name: 'Claude Code', status: 'ok' }),
      git: check({ name: 'Git', status: 'ok' }),
      openrouter: check({ name: 'OpenRouter', status: 'ok' }),
      nvidia: check({ name: 'NVIDIA NIM', status: 'ok' }),
      rojo: check({ name: 'Rojo', status: 'ok' }),
      studioMcp: check({ name: 'Roblox Studio MCP', status: 'ok' }),
    },
    checks: [
      check({ name: 'database', status: 'ok' }),
      check({ name: 'dataDirectory', status: 'ok' }),
    ],
    ...overrides,
  };
}

describe('buildWizardChecks + deriveGates', () => {
  it('reports nothing blocking and a usable provider when everything is ok', () => {
    const checks = buildWizardChecks(diagnostics());
    const gates = deriveGates(checks, { mockMode: false });
    expect(gates.blocked).toBe(false);
    expect(gates.hasUsableProvider).toBe(true);
    expect(gates.providersNeedAttention).toEqual([]);
    expect(gates.missingOptional).toEqual([]);
  });

  it('treats a warning provider as distinct from missing, and as not usable on its own', () => {
    const diag = diagnostics({
      dependencies: {
        claude: check({ name: 'Claude Code', status: 'warning' }),
      },
    });
    const checks = buildWizardChecks(diag);
    const claude = checks.find((c) => c.id === 'claude')!;
    expect(claude.status).toBe('warning');
    expect(claude.severity).toBe('provider');
    const gates = deriveGates(checks, { mockMode: false });
    expect(gates.hasUsableProvider).toBe(false);
    expect(gates.providersNeedAttention.map((c) => c.id)).toEqual(['claude']);
    expect(gates.blocked).toBe(false);
  });

  it('marks a missing openrouter dependency as provider severity, not blocking', () => {
    const diag = diagnostics({
      dependencies: { openrouter: check({ name: 'OpenRouter', status: 'missing' }) },
    });
    const checks = buildWizardChecks(diag);
    const openrouter = checks.find((c) => c.id === 'openrouter')!;
    expect(openrouter.status).toBe('missing');
    expect(openrouter.severity).toBe('provider');
    expect(openrouter.fixableInSettings).toBe(true);
    expect(openrouter.settingsAnchor).toBe('settings-openrouter');
    const gates = deriveGates(checks, { mockMode: false });
    expect(gates.hasUsableProvider).toBe(false);
    expect(gates.blocked).toBe(false);
  });

  it('marks a missing nvidia dependency as provider severity, fixable under its own anchor', () => {
    const diag = diagnostics({
      dependencies: { nvidia: check({ name: 'NVIDIA NIM', status: 'missing' }) },
    });
    const checks = buildWizardChecks(diag);
    const nvidia = checks.find((c) => c.id === 'nvidia')!;
    expect(nvidia.status).toBe('missing');
    expect(nvidia.severity).toBe('provider');
    expect(nvidia.fixableInSettings).toBe(true);
    expect(nvidia.settingsAnchor).toBe('settings-nvidia');
  });

  it('treats missing rojo/studioMcp as non-blocking integrations (limited mode)', () => {
    const diag = diagnostics({
      dependencies: {
        ...diagnostics().dependencies,
        rojo: check({ name: 'Rojo', status: 'missing' }),
        studioMcp: check({ name: 'Roblox Studio MCP', status: 'missing' }),
      },
    });
    const checks = buildWizardChecks(diag);
    const rojo = checks.find((c) => c.id === 'rojo')!;
    const studioMcp = checks.find((c) => c.id === 'studioMcp')!;
    expect(rojo.severity).toBe('integration');
    expect(studioMcp.severity).toBe('integration');
    const gates = deriveGates(checks, { mockMode: false });
    expect(gates.blocked).toBe(false);
    expect(gates.missingOptional.map((c) => c.id).sort()).toEqual(['rojo', 'studioMcp']);
  });

  it('blocks on a database error regardless of provider status', () => {
    const diag = diagnostics({
      checks: [
        check({ name: 'database', status: 'error' }),
        check({ name: 'dataDirectory', status: 'ok' }),
      ],
    });
    const checks = buildWizardChecks(diag);
    const gates = deriveGates(checks, { mockMode: false });
    expect(gates.blocked).toBe(true);
    expect(gates.hasUsableProvider).toBe(true);
  });

  it('blocks on a dataDirectory error', () => {
    const diag = diagnostics({
      checks: [
        check({ name: 'database', status: 'ok' }),
        check({ name: 'dataDirectory', status: 'error' }),
      ],
    });
    const checks = buildWizardChecks(diag);
    const gates = deriveGates(checks, { mockMode: false });
    expect(gates.blocked).toBe(true);
  });

  it('treats mock mode as always having a usable provider, even with none configured', () => {
    const diag = diagnostics({
      dependencies: {
        claude: check({ name: 'Claude Code', status: 'missing' }),
        openrouter: check({ name: 'OpenRouter', status: 'missing' }),
        nvidia: check({ name: 'NVIDIA NIM', status: 'missing' }),
      },
      mockMode: true,
    });
    const checks = buildWizardChecks(diag);
    const gates = deriveGates(checks, { mockMode: true });
    expect(gates.hasUsableProvider).toBe(true);
  });

  it('falls back an unknown dependency id to integration severity with no known label', () => {
    const diag = diagnostics({
      dependencies: {
        ...diagnostics().dependencies,
        futureTool: check({ name: 'Future Tool', status: 'missing' }),
      },
    });
    const checks = buildWizardChecks(diag);
    const unknown = checks.find((c) => c.id === 'futureTool')!;
    expect(unknown.severity).toBe('integration');
    expect(unknown.labelKey).toBeUndefined();
    expect(unknown.fixableInSettings).toBe(false);
  });
});

describe('completionTarget', () => {
  it('sends an empty project list to new-project', () => {
    expect(completionTarget([])).toBe('new-project');
  });
  it('sends an all-archived project list to new-project', () => {
    expect(completionTarget([{ archived: true }])).toBe('new-project');
  });
  it('sends a list with an active project to projects', () => {
    expect(completionTarget([{ archived: true }, { archived: false }])).toBe('projects');
  });
});
