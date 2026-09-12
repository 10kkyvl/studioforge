import type { Task } from './types';

// TaskBlocker mirrors internal/tasks.Blocker for display purposes: one
// dependency, direct or transitive, that is not yet "completed" (or does not
// resolve to a same-project task at all — status "missing").
export type TaskBlocker = {
  taskId: string;
  title: string;
  status: string;
};

export type TaskGraphEdge = {
  from: string;
  to: string;
};

export type TaskGraphNode = {
  task: Task;
  depth: number;
};

export type TaskGraph = {
  nodes: TaskGraphNode[];
  edges: TaskGraphEdge[];
};

export type TaskGraphRelations = {
  blockers: string[];
  dependents: string[];
};

// taskReadiness mirrors the backend's internal/tasks.TaskReadiness for
// display only: the server is the sole authority on whether a run may
// actually start. It walks taskId's dependency graph, direct and transitive,
// using only the tasks already present in the given list (the same snapshot
// the UI already has), and reports every dependency that has not reached
// "completed". A dependency id that is not present in tasks — deleted, or
// belonging to a project not represented in this list — is reported as a
// blocker with status "missing" and an empty title, matching the backend's
// refusal to leak another project's task details. Cycles are tolerated (each
// task is visited once) so stray cyclic data can never hang this call.
export function taskReadiness(
  tasks: Task[],
  taskId: string,
): { ready: boolean; blockers: TaskBlocker[] } {
  const byId = new Map(tasks.map((task) => [task.id, task]));
  const root = byId.get(taskId);
  if (!root) return { ready: true, blockers: [] };

  const visited = new Set<string>([taskId]);
  const blockers: TaskBlocker[] = [];

  function walk(id: string): void {
    if (visited.has(id)) return;
    visited.add(id);
    const dep = byId.get(id);
    if (!dep || dep.projectId !== root!.projectId) {
      blockers.push({ taskId: id, title: '', status: 'missing' });
      return;
    }
    if (dep.status !== 'completed') {
      blockers.push({ taskId: dep.id, title: dep.title, status: dep.status });
    }
    for (const next of dep.dependencies) walk(next);
  }

  for (const depId of root.dependencies) walk(depId);
  return { ready: blockers.length === 0, blockers };
}

// isTaskBlocked is the common case: does this task have any unfinished
// dependency at all.
export function isTaskBlocked(tasks: Task[], taskId: string): boolean {
  return !taskReadiness(tasks, taskId).ready;
}

// A task is ready to start only while it is waiting in the board and every
// direct and transitive dependency is completed. Running, review, completed,
// and failed tasks remain visible in the normal board but are not candidates
// for the operator's "Ready to start" filter.
export function isTaskReadyToStart(tasks: Task[], taskId: string): boolean {
  const task = tasks.find((candidate) => candidate.id === taskId);
  return (
    !!task &&
    (task.status === 'backlog' || task.status === 'ready') &&
    taskReadiness(tasks, taskId).ready
  );
}

// taskDependencyGraph builds the read-only graph shown by the task board. The
// graph is intentionally dependency-scoped: an edge is included only when
// both tasks belong to the same project and are present in the snapshot. This
// keeps an all-projects view from drawing a misleading cross-project edge or
// leaking a task that readiness would report as missing.
export function taskDependencyGraph(tasks: Task[], projectId?: string): TaskGraph {
  const scoped = projectId ? tasks.filter((task) => task.projectId === projectId) : tasks;
  const byId = new Map(scoped.map((task) => [task.id, task]));
  const edges: TaskGraphEdge[] = [];
  for (const task of scoped) {
    for (const dependencyId of task.dependencies) {
      const dependency = byId.get(dependencyId);
      if (dependency && dependency.projectId === task.projectId) {
        edges.push({ from: dependency.id, to: task.id });
      }
    }
  }

  const depthMemo = new Map<string, number>();
  function depth(id: string, visiting = new Set<string>()): number {
    const memoized = depthMemo.get(id);
    if (memoized !== undefined) return memoized;
    if (visiting.has(id)) return 0;
    const task = byId.get(id);
    if (!task) return 0;
    const nextVisiting = new Set(visiting).add(id);
    const dependencyDepths = task.dependencies
      .filter((dependencyId) => byId.get(dependencyId)?.projectId === task.projectId)
      .map((dependencyId) => depth(dependencyId, nextVisiting));
    const result = dependencyDepths.length ? Math.max(...dependencyDepths) + 1 : 0;
    depthMemo.set(id, result);
    return result;
  }

  const nodes = scoped
    .map((task) => ({ task, depth: depth(task.id) }))
    .sort((a, b) => a.depth - b.depth || a.task.title.localeCompare(b.task.title));
  return { nodes, edges };
}

// taskGraphRelations returns every ancestor and descendant of taskId. Unlike
// taskReadiness, it includes completed tasks too: selection is a comprehension
// aid, so the operator can see the complete upstream and downstream path.
export function taskGraphRelations(tasks: Task[], taskId: string): TaskGraphRelations {
  const root = tasks.find((task) => task.id === taskId);
  if (!root) return { blockers: [], dependents: [] };
  const rootProjectId = root.projectId;
  const byId = new Map(tasks.map((task) => [task.id, task]));
  const blockersByTask = new Map<string, string[]>();
  for (const task of tasks) {
    if (task.projectId !== rootProjectId) continue;
    for (const dependencyId of task.dependencies) {
      const dependency = byId.get(dependencyId);
      if (dependency?.projectId === task.projectId) {
        blockersByTask.set(dependencyId, [...(blockersByTask.get(dependencyId) ?? []), task.id]);
      }
    }
  }

  function walk(start: string, links: Map<string, string[]>): string[] {
    const seen = new Set<string>();
    const pending = [...(links.get(start) ?? [])];
    while (pending.length) {
      const id = pending.shift()!;
      if (seen.has(id) || id === taskId) continue;
      const task = byId.get(id);
      if (!task || task.projectId !== rootProjectId) continue;
      seen.add(id);
      pending.push(...(links.get(id) ?? []));
    }
    return [...seen];
  }

  const dependencyLinks = new Map<string, string[]>();
  for (const task of tasks) {
    if (task.projectId === rootProjectId) {
      dependencyLinks.set(
        task.id,
        task.dependencies.filter((id) => byId.get(id)?.projectId === rootProjectId),
      );
    }
  }
  return {
    blockers: walk(taskId, dependencyLinks),
    dependents: walk(taskId, blockersByTask),
  };
}
