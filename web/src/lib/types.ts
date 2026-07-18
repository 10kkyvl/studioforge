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
export type Project = {
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
// Token counters a provider reported. The cache fields are Claude-only and
// stay 0 elsewhere; they are separate from inputTokens because Claude counts
// cache hits outside it.
export type TokenUsage = {
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
};
export type Run = TokenUsage & {
  id: string;
  projectId: string;
  agentId: string;
  taskId?: string;
  provider: string;
  modelAlias: string;
  status: string;
  phase: string;
  requiredResource?: string;
  error?: string;
  cost: number;
  createdAt: string;
  updatedAt: string;
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
export type ChatThread = {
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
};
export type Decision = {
  id: string;
  projectId: string;
  title: string;
  reason: string;
  proposedAction: string;
  risk: string;
  preview: string;
  status: string;
  resolution?: string;
  createdAt: string;
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
export type Snapshot = {
  projects: Project[];
  runs: Run[];
  agents: Agent[];
  tasks: Task[];
  decisions: Decision[];
  studios: StudioSession[];
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
  codex_path: string;
  claude_path: string;
  rojo_path: string;
  git_path: string;
  studio_mcp_path: string;
  studio_auto_open: string;
  concurrency: string;
};
