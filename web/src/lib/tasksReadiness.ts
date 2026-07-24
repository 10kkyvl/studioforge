import type { Task } from './types';

// TaskBlocker mirrors internal/tasks.Blocker for display purposes: one
// dependency, direct or transitive, that is not yet "completed" (or does not
// resolve to a same-project task at all — status "missing").
export type TaskBlocker = {
  taskId: string;
  title: string;
  status: string;
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
