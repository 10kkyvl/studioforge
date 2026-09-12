import { describe, expect, it } from 'vitest';
import type { Task } from './types';
import {
  isTaskBlocked,
  isTaskReadyToStart,
  taskDependencyGraph,
  taskGraphRelations,
  taskReadiness,
} from './tasksReadiness';

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

describe('isTaskReadyToStart', () => {
  it('accepts waiting tasks with completed transitive dependencies', () => {
    const tasks = [
      task({ id: 'a', status: 'completed' }),
      task({ id: 'b', status: 'ready', dependencies: ['a'] }),
    ];
    expect(isTaskReadyToStart(tasks, 'b')).toBe(true);
  });

  it('excludes active and completed tasks even when dependencies are satisfied', () => {
    const tasks = [
      task({ id: 'running', status: 'running' }),
      task({ id: 'completed', status: 'completed' }),
    ];
    expect(isTaskReadyToStart(tasks, 'running')).toBe(false);
    expect(isTaskReadyToStart(tasks, 'completed')).toBe(false);
  });
});

describe('taskDependencyGraph', () => {
  it('returns dependency edges and layers nodes by dependency depth', () => {
    const tasks = [
      task({ id: 'a', title: 'A' }),
      task({ id: 'b', title: 'B', dependencies: ['a'] }),
      task({ id: 'c', title: 'C', dependencies: ['b'] }),
      task({ id: 'other', projectId: 'p2', dependencies: ['a'] }),
    ];
    expect(taskDependencyGraph(tasks, 'p1')).toEqual({
      nodes: [
        { task: tasks[0], depth: 0 },
        { task: tasks[1], depth: 1 },
        { task: tasks[2], depth: 2 },
      ],
      edges: [
        { from: 'a', to: 'b' },
        { from: 'b', to: 'c' },
      ],
    });
  });

  it('omits missing and cross-project dependency edges', () => {
    const tasks = [
      task({ id: 'a', projectId: 'p2' }),
      task({ id: 'b', dependencies: ['a', 'deleted'] }),
    ];
    expect(taskDependencyGraph(tasks, 'p1')).toEqual({
      nodes: [{ task: tasks[1], depth: 0 }],
      edges: [],
    });
  });

  it('does not use a foreign dependency when calculating all-project depth', () => {
    const tasks = [
      task({ id: 'foreign-base', projectId: 'p2' }),
      task({ id: 'foreign', projectId: 'p2', dependencies: ['foreign-base'] }),
      task({ id: 'local', projectId: 'p1', dependencies: ['foreign'] }),
    ];
    expect(taskDependencyGraph(tasks).nodes.find((node) => node.task.id === 'local')?.depth).toBe(
      0,
    );
  });
});

describe('taskGraphRelations', () => {
  it('walks all ancestors and descendants, including completed tasks', () => {
    const tasks = [
      task({ id: 'root', dependencies: ['up'] }),
      task({ id: 'up', status: 'completed', dependencies: ['base'] }),
      task({ id: 'base', status: 'completed' }),
      task({ id: 'child', dependencies: ['root'] }),
      task({ id: 'grandchild', dependencies: ['child'] }),
    ];
    expect(taskGraphRelations(tasks, 'root')).toEqual({
      blockers: ['up', 'base'],
      dependents: ['child', 'grandchild'],
    });
  });

  it('ignores cross-project links and tolerates cycles', () => {
    const tasks = [
      task({ id: 'a', dependencies: ['b', 'foreign'] }),
      task({ id: 'b', dependencies: ['a'] }),
      task({ id: 'foreign', projectId: 'p2', dependencies: ['a'] }),
    ];
    expect(taskGraphRelations(tasks, 'a')).toEqual({ blockers: ['b'], dependents: ['b'] });
  });
});
