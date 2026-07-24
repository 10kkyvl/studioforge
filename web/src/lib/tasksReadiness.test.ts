import { describe, expect, it } from 'vitest';
import type { Task } from './types';
import { isTaskBlocked, taskReadiness } from './tasksReadiness';

function task(patch: Partial<Task>): Task {
  return {
    id: 't1',
    projectId: 'p1',
    title: 'Task',
    description: '',
    acceptanceCriteria: '',
    priority: 50,
    status: 'backlog',
    dependencies: [],
    ...patch,
  };
}

describe('taskReadiness', () => {
  it('is ready when there are no dependencies', () => {
    const tasks = [task({ id: 'a' })];
    expect(taskReadiness(tasks, 'a')).toEqual({ ready: true, blockers: [] });
  });

  it('is ready when every direct dependency is completed', () => {
    const tasks = [task({ id: 'a', status: 'completed' }), task({ id: 'b', dependencies: ['a'] })];
    expect(taskReadiness(tasks, 'b')).toEqual({ ready: true, blockers: [] });
  });

  it('is blocked by an incomplete direct dependency', () => {
    const tasks = [
      task({ id: 'a', title: 'A', status: 'running' }),
      task({ id: 'b', dependencies: ['a'] }),
    ];
    expect(taskReadiness(tasks, 'b')).toEqual({
      ready: false,
      blockers: [{ taskId: 'a', title: 'A', status: 'running' }],
    });
  });

  it('is blocked by a transitive incomplete dependency, even under a completed one', () => {
    const tasks = [
      task({ id: 'a', title: 'A', status: 'running' }),
      task({ id: 'b', title: 'B', status: 'completed', dependencies: ['a'] }),
      task({ id: 'c', dependencies: ['b'] }),
    ];
    const result = taskReadiness(tasks, 'c');
    expect(result.ready).toBe(false);
    expect(result.blockers).toEqual([{ taskId: 'a', title: 'A', status: 'running' }]);
  });

  it('reports a dependency missing from the snapshot as blocked with status missing and no title', () => {
    const tasks = [task({ id: 'b', dependencies: ['deleted'] })];
    expect(taskReadiness(tasks, 'b')).toEqual({
      ready: false,
      blockers: [{ taskId: 'deleted', title: '', status: 'missing' }],
    });
  });

  it('treats a dependency belonging to another project as missing, withholding its title', () => {
    const tasks = [
      task({ id: 'a', projectId: 'p2', title: 'Other project secret', status: 'completed' }),
      task({ id: 'b', projectId: 'p1', dependencies: ['a'] }),
    ];
    expect(taskReadiness(tasks, 'b')).toEqual({
      ready: false,
      blockers: [{ taskId: 'a', title: '', status: 'missing' }],
    });
  });

  it('treats an unknown root task id as trivially ready', () => {
    expect(taskReadiness([], 'nope')).toEqual({ ready: true, blockers: [] });
  });

  it('tolerates a dependency cycle without hanging', () => {
    const tasks = [task({ id: 'a', dependencies: ['b'] }), task({ id: 'b', dependencies: ['a'] })];
    const result = taskReadiness(tasks, 'a');
    expect(result.ready).toBe(false);
  });
});

describe('isTaskBlocked', () => {
  it('mirrors taskReadiness.ready, negated', () => {
    const tasks = [
      task({ id: 'a', status: 'completed' }),
      task({ id: 'b', status: 'running' }),
      task({ id: 'c', dependencies: ['a'] }),
      task({ id: 'd', dependencies: ['b'] }),
    ];
    expect(isTaskBlocked(tasks, 'c')).toBe(false);
    expect(isTaskBlocked(tasks, 'd')).toBe(true);
  });
});
