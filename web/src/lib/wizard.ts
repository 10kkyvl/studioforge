import type { TranslationKey } from './i18n';
import type { Check, Diagnostics } from './types';

export type WizardStatus = 'ok' | 'warning' | 'error' | 'missing';
export type WizardSeverity = 'blocker' | 'provider' | 'integration';

export type WizardCheck = {
  id: string;
  status: WizardStatus;
  severity: WizardSeverity;
  labelKey?: TranslationKey;
  helpKey?: TranslationKey;
  messageOverride?: TranslationKey;
  version?: string;
  path?: string;
  fixableInSettings: boolean;
  settingsAnchor?: string;
  raw: Check;
};

export type WizardGates = {
  blocked: boolean;
  hasUsableProvider: boolean;
  providersNeedAttention: WizardCheck[];
  missingOptional: WizardCheck[];
};

export const wizardLabelKeys: Record<string, TranslationKey> = {
  claude: 'check.claude',
  git: 'check.git',
  openrouter: 'check.openrouter',
  nvidia: 'check.nvidia',
  rojo: 'check.rojo',
  studioMcp: 'check.studioMcp',
  mcp: 'check.studioMcp',
  database: 'check.database',
  dataDirectory: 'check.dataDirectory',
};

export const wizardHelpKeys: Record<string, TranslationKey> = {
  'Install Git or configure its executable path in Settings.': 'check.gitHelp',
  'Run `claude auth status`, then authenticate with Claude Code if needed.': 'check.claudeHelp',
  'Install Rojo 7 from the official Rojo documentation.': 'check.rojoHelp',
  'Update Roblox Studio, open Assistant settings, and enable Studio as MCP server.':
    'check.studioMcpHelp',
  'Add your OpenRouter API key in Settings and click Test connection.': 'check.openrouterHelp',
};

export const wizardMessageKeys: Record<string, TranslationKey> = {
  'Claude Code detected': 'check.claudeDetected',
  'Claude Code was not found. Install it, then run StudioForge doctor. Mock mode remains available.':
    'check.claudeNotFound',
  'Rojo CLI detected': 'check.rojoDetected',
  'Rojo CLI not found; install Rojo 7 and ensure rojo is on PATH': 'check.rojoNotFound',
  'Official Studio MCP launcher detected': 'check.studioMcpDetected',
};

const severityById: Record<string, WizardSeverity> = {
  database: 'blocker',
  dataDirectory: 'blocker',
  claude: 'provider',
  openrouter: 'provider',
  nvidia: 'provider',
  git: 'integration',
  rojo: 'integration',
  studioMcp: 'integration',
  mcp: 'integration',
};

const settingsAnchorById: Record<string, string> = {
  claude: 'settings-integrations',
  git: 'settings-integrations',
  rojo: 'settings-integrations',
  studioMcp: 'settings-integrations',
  mcp: 'settings-integrations',
  openrouter: 'settings-openrouter',
  nvidia: 'settings-nvidia',
};

export function normalizeStatus(raw: string): WizardStatus {
  if (raw === 'ok' || raw === 'warning' || raw === 'error' || raw === 'missing') return raw;
  return 'missing';
}

function toWizardCheck(id: string, check: Check): WizardCheck {
  const anchor = settingsAnchorById[id];
  return {
    id,
    status: normalizeStatus(check.status),
    severity: severityById[id] ?? 'integration',
    labelKey: wizardLabelKeys[id],
    helpKey: check.help ? wizardHelpKeys[check.help] : undefined,
    messageOverride: check.message ? wizardMessageKeys[check.message] : undefined,
    version: check.version,
    path: check.path,
    fixableInSettings: Boolean(anchor),
    settingsAnchor: anchor,
    raw: check,
  };
}

export function buildWizardChecks(diagnostics: Diagnostics): WizardCheck[] {
  const dependencyChecks = Object.entries(diagnostics.dependencies).map(([id, check]) =>
    toWizardCheck(id, check),
  );
  const freeChecks = diagnostics.checks.map((check) => toWizardCheck(check.name, check));
  return [...dependencyChecks, ...freeChecks];
}

export function deriveGates(checks: WizardCheck[], opts: { mockMode: boolean }): WizardGates {
  const blockers = checks.filter((check) => check.severity === 'blocker');
  const providers = checks.filter((check) => check.severity === 'provider');
  const optional = checks.filter((check) => check.severity !== 'blocker');
  return {
    blocked: blockers.some((check) => check.status === 'error'),
    hasUsableProvider: opts.mockMode || providers.some((check) => check.status === 'ok'),
    providersNeedAttention: providers.filter(
      (check) => check.status === 'warning' || check.status === 'error',
    ),
    missingOptional: optional.filter((check) => check.status !== 'ok'),
  };
}

export function completionTarget(projects: { archived?: boolean }[]): 'new-project' | 'projects' {
  return projects.some((project) => !project.archived) ? 'projects' : 'new-project';
}

export function checkLabel(
  t: (key: TranslationKey) => string,
  id: string,
  fallbackName: string,
): string {
  const key = wizardLabelKeys[id];
  return key ? t(key) : fallbackName || id;
}

export function checkHelpLabel(t: (key: TranslationKey) => string, help: string): string {
  const key = wizardHelpKeys[help];
  return key ? t(key) : help;
}

export function checkMessageLabel(t: (key: TranslationKey) => string, message: string): string {
  const key = wizardMessageKeys[message];
  return key ? t(key) : message;
}

export function checkStatusLabel(t: (key: TranslationKey) => string, status: string): string {
  const key = `state.${status}` as TranslationKey;
  return t(key) || status;
}
