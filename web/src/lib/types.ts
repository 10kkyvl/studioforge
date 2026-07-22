export type Check = {
  name: string;
  status: string;
  version?: string;
  path?: string;
  message?: string;
  help?: string;
};
export type Diagnostics = {
  version: string;
  os: string;
  arch: string;
  dataPath: string;
  database: string;
  wal: boolean;
  fts5: boolean;
  safeMode: boolean;
  mockMode: boolean;
  dependencies: Record<string, Check>;
  checks: Check[];
};
// Token counters a provider reported. The cache fields are Claude-only and
// stay 0 elsewhere; they are separate from inputTokens because Claude counts
// cache hits outside it.
export type TokenUsage = {
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
};
// A project's live Rojo `rojo serve` session, if any. Rides on the project
// payload rather than its own polling endpoint (contrast StudioStatus below)
// because it only changes in response to the sync endpoints or the session
// dying on its own.
export type SyncStatus = {
  active: boolean;
  port: number;
  startedAt: string;
  // The session's most recent `rojo serve` log lines, oldest first.
  recentLogs?: string[];
};
// Project's TokenUsage is not one run's counters but the SUM across every run
// in the project, so the project card can show total spend without re-summing
// runs on the client.
export type Project = TokenUsage & {
  id: string;
  name: string;
  path: string;
  description: string;
  groupName?: string;
  tags: string[];
  pinned: boolean;
  archived: boolean;
  mock: boolean;
  budgetLimit: number;
  budgetUsed: number;
  runningAgents: number;
  sync: SyncStatus;
  updatedAt: string;
};
export type Agent = {
  id: string;
  projectId: string;
  name: string;
  role: string;
  provider: string;
  modelAlias: string;
  effort: string;
  enabled: boolean;
  permission: string;
  concurrency: number;
  budget: number;
  // Opts this agent into the post-run Studio playtest validation loop
  // (Claude runs only, workspace-write permission or above). Off by default.
  validateAfterRun: boolean;
  maxCorrectionRuns: number;
};
export type Task = {
  id: string;
  projectId: string;
  title: string;
  description: string;
  acceptanceCriteria: string;
  priority: number;
  status: string;
  assignedAgentId?: string;
  dependencies: string[];
  blockedReason?: string;
};
export type Run = TokenUsage & {
  id: string;
  projectId: string;
  agentId: string;
  taskId?: string;
  threadId?: string;
  provider: string;
  modelAlias: string;
  status: string;
  phase: string;
  requiredResource?: string;
  error?: string;
  cost: number;
  createdAt: string;
  updatedAt: string;
  // The Studio playtest validation outcome: none, passed, failed,
  // inconclusive, corrected, or correction_failed.
  validation: string;
  validationScreenshot?: string;
  // Set on a correction run: the run whose failed validation scheduled it.
  parentRunId?: string;
  correctionDepth: number;
};
export type RunDiff = {
  diff: string;
  status?: string;
  note?: string;
  checkpoint?: {
    commitHash: string;
    branch: string;
    label: string;
    createdAt: string;
  };
};
export type RunEvent = {
  id: number;
  projectId: string;
  runId: string;
  agentId?: string;
  type: string;
  rawType?: string;
  payload: unknown;
  createdAt: string;
};
// Same aggregate as Project's TokenUsage, scoped to this thread's runs
// instead of the whole project.
export type ChatThread = TokenUsage & {
  id: string;
  projectId: string;
  title: string;
  createdAt: string;
  updatedAt: string;
};
export type ChatMessage = {
  role: 'user' | 'agent';
  text: string;
  at: string;
  runId: string;
  status?: string;
  // Survives from the scheduler event's own RawType (e.g. "scheduler.stuck")
  // when this message was scheduler-synthesized rather than the agent's own
  // text, so a stuck-escalation question card can be told apart from the
  // agent's own natural question identically live and after a reload.
  rawType?: string;
};
export type StudioSession = {
  id: string;
  projectId?: string;
  instanceId: string;
  name: string;
  placeId?: string;
  gameId?: string;
  active: boolean;
  playState: string;
  mock: boolean;
  lastSeenAt: string;
};
export type StudioStatus = {
  open: number;
  matched: number;
  // 'blocked' means Studio is running but another MCP client owns its
  // connection, which the launcher otherwise reports the same as 'none'.
  state: 'matched' | 'other' | 'none' | 'blocked';
  blocked?: boolean;
  error?: string;
};
// An operator-approval gate: something the scheduler would otherwise have
// silently decided on its own (today, only a correction run whose automatic
// budget was exhausted). Only pending decisions ride on the snapshot.
export type Decision = {
  id: string;
  projectId: string;
  runId: string;
  kind: string;
  summary: string;
  detail?: string;
  status: string;
  createdAt: string;
  resolvedAt?: string;
};
export type Snapshot = {
  projects: Project[];
  runs: Run[];
  agents: Agent[];
  tasks: Task[];
  studios: StudioSession[];
  decisions: Decision[];
  diagnostics: Diagnostics;
  settings: AppSettings;
};
export type ToolCandidate = {
  path: string;
  version?: string;
  source: string;
  status: string;
  message?: string;
};
export type DetectedPaths = Record<string, ToolCandidate[]>;
export type AppSettings = {
  locale: string;
  setupComplete: boolean;
  safeMode: boolean;
  default_provider: string;
  default_model: string;
  default_effort: string;
  claude_path: string;
  rojo_path: string;
  git_path: string;
  studio_mcp_path: string;
  studio_auto_open: string;
  concurrency: string;
  playtest_window_seconds: string;
  // OpenRouter routing preferences. Empty string means "provider default" for
  // every field; require_parameters has no UI toggle and is always on
  // server-side. Persisted through the same POST /settings payload as every
  // other field above, not a dedicated endpoint.
  openrouter_data_collection: string;
  openrouter_zdr: string;
  openrouter_allow_fallbacks: string;
};

// The stored-key lifecycle: no key saved yet, a key was saved but never
// exercised against the API, a key that answered a real request, or a key
// the API rejected.
export type OpenRouterKeyState = 'not_configured' | 'unverified' | 'configured' | 'invalid';
// Where the active key came from. 'keychain' is the only source the UI can
// treat as durable — 'session' and 'env' both disappear once the process
// backing them goes away, which is why `secure` rides alongside separately
// rather than being inferred from this field alone.
export type OpenRouterKeySource = 'keychain' | 'session' | 'env' | 'none';
export type OpenRouterStatus = {
  state: OpenRouterKeyState;
  source: OpenRouterKeySource;
  secure: boolean;
};
export type OpenRouterKeyTestResult = OpenRouterStatus & { ok: boolean };
export type OpenRouterModel = {
  id: string;
  name: string;
  contextLength: number;
  vision: boolean;
  tools: boolean;
  structured: boolean;
  free: boolean;
  promptPrice: number;
  completionPrice: number;
};
// A hand-picked subset of `models`, grouped by `category` for the picker.
// `available` can be false (e.g. a curated pick OpenRouter has since pulled)
// without the entry disappearing, so the picker can grey it out instead of
// silently dropping a choice the operator may already have saved.
export type OpenRouterCurated = {
  id: string;
  category: string;
  recommendation: string;
  workload: string;
  free: boolean;
  vision: boolean;
  available: boolean;
};
export type OpenRouterModelsResponse = {
  source: 'live' | 'cache' | 'fallback';
  models: OpenRouterModel[];
  curated: OpenRouterCurated[];
  categories: string[];
};
export type OpenRouterCapabilities = {
  known: boolean;
  vision: boolean;
  tools: boolean;
  structured: boolean;
  contextLength: number;
  free: boolean;
};
