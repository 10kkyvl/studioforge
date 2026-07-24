<script lang="ts">
  import { Lock, Plus, X } from '@lucide/svelte';
  import { translate, type TranslationKey } from '$lib/i18n';
  import { isTaskBlocked } from '$lib/tasksReadiness';
  import type { Project, Task } from '$lib/types';

  export let tasks: Task[] = [];
  export let project: Project | undefined = undefined;
  export let onCreateTask: (t: {
    title: string;
    status: string;
    dependencies?: string[];
  }) => void = () => {};
  export let onUpdateStatus: (taskId: string, status: string) => void = () => {};
  export let onDeleteTask: (taskId: string) => void = () => {};

  const columns = [
    { key: 'backlog', label: 'tasks.backlog' },
    { key: 'ready', label: 'tasks.ready' },
    { key: 'blocked', label: 'tasks.blocked' },
    { key: 'running', label: 'tasks.running' },
    { key: 'review', label: 'tasks.review' },
    { key: 'completed', label: 'tasks.completed' },
  ] as const;

  let draggedTaskId: string | null = null;
  let showNewTask = false;
  let newTaskTitle = '';
  let newTaskDependencies: string[] = [];

  $: visibleTasks = project ? tasks.filter((task) => task.projectId === project?.id) : tasks;

  // Group reactively so the board re-renders when tasks change. A template that
  // called a tasksIn(column.key) helper hid the visibleTasks dependency from
  // Svelte, so created/moved/deleted tasks only appeared after a full reload.
  $: grouped = Object.fromEntries(
    columns.map((column) => [
      column.key,
      visibleTasks.filter((task) => task.status === column.key),
    ]),
  ) as Record<string, Task[]>;

  function submitNewTask() {
    const title = newTaskTitle.trim();
    if (!title) return;
    onCreateTask({
      title,
      status: 'backlog',
      ...(newTaskDependencies.length ? { dependencies: newTaskDependencies } : {}),
    });
    newTaskTitle = '';
    newTaskDependencies = [];
    showNewTask = false;
  }

  function toggleDependency(taskId: string, checked: boolean) {
    newTaskDependencies = checked
      ? [...newTaskDependencies, taskId]
      : newTaskDependencies.filter((id) => id !== taskId);
  }

  function handleDragStart(event: DragEvent, taskId: string) {
    draggedTaskId = taskId;
    if (event.dataTransfer) {
      event.dataTransfer.setData('text/plain', taskId);
      event.dataTransfer.effectAllowed = 'move';
    }
  }

  function handleDrop(event: DragEvent, status: string) {
    event.preventDefault();
    const id = draggedTaskId || event.dataTransfer?.getData('text/plain') || '';
    if (id) onUpdateStatus(id, status);
    draggedTaskId = null;
  }

  // Dependency titles and statuses are resolved against the full tasks list,
  // not visibleTasks, since a project filter must not hide the fact that a
  // dependency exists — only cross-project *resolution* is refused, matching
  // the backend's TaskReadiness (a dependency belonging to a different
  // project is reported as missing, its details withheld).
  function dependencyChip(
    taskProjectId: string,
    dependsOnId: string,
  ): { label: string; status: string } {
    const dep = tasks.find((task) => task.id === dependsOnId);
    if (!dep || dep.projectId !== taskProjectId) {
      return { label: '', status: 'missing' };
    }
    return { label: dep.title, status: dep.status };
  }

  function taskStatusLabel(status: string): string {
    const key = `tasks.status.${status}` as TranslationKey;
    return $translate(key) || status;
  }
</script>

<section class="page-heading" data-testid="tasks-view">
  <div>
    <p class="eyebrow">{$translate('nav.tasks')}</p>
    <h1>{$translate('tasks.title')}</h1>
    <p>{$translate('tasks.subtitle')}</p>
    {#if !project}<span class="chip">{$translate('common.allProjects')}</span>{/if}
  </div>
  <button class="primary" type="button" onclick={() => (showNewTask = !showNewTask)}
    ><Plus size={17} />{$translate('tasks.new')}</button
  >
</section>
{#if showNewTask}
  <form
    class="new-task-form"
    onsubmit={(event) => {
      event.preventDefault();
      submitNewTask();
    }}
  >
    <div class="new-task-row">
      <input
        bind:value={newTaskTitle}
        placeholder={$translate('tasks.titlePlaceholder')}
        required
      />
      <button class="primary" type="submit"><Plus size={15} />{$translate('tasks.new')}</button>
    </div>
    {#if visibleTasks.length > 0}
      <fieldset class="dependency-picker">
        <legend>{$translate('tasks.depends')}</legend>
        {#each visibleTasks as task (task.id)}
          <label>
            <input
              type="checkbox"
              checked={newTaskDependencies.includes(task.id)}
              onchange={(event) => toggleDependency(task.id, event.currentTarget.checked)}
            />
            {task.title}
          </label>
        {/each}
      </fieldset>
    {/if}
  </form>
{/if}
{#if visibleTasks.length === 0}
  <div class="empty">
    <p>{$translate('tasks.empty')}</p>
  </div>
{:else}
  <section class="board">
    {#each columns as column (column.key)}
      <div
        class="board-column"
        role="list"
        aria-label={$translate(column.label as TranslationKey)}
        ondragover={(event) => event.preventDefault()}
        ondrop={(event) => handleDrop(event, column.key)}
      >
        <header>
          <h2>{$translate(column.label as TranslationKey)}</h2>
          <span>{(grouped[column.key] ?? []).length}</span>
        </header>
        {#each grouped[column.key] ?? [] as task (task.id)}
          {@const depsBlocked = isTaskBlocked(tasks, task.id)}
          <article
            class="board-card"
            class:is-blocked={task.status === 'blocked'}
            class:is-deps-blocked={depsBlocked}
            role="listitem"
            draggable="true"
            ondragstart={(event) => handleDragStart(event, task.id)}
          >
            <div class="board-card-top">
              <span class="priority">P{task.priority}</span>
              <button
                type="button"
                class="delete-task"
                aria-label={$translate('tasks.delete')}
                title={$translate('tasks.delete')}
                onclick={() => onDeleteTask(task.id)}><X size={13} /></button
              >
            </div>
            <h3>
              {#if depsBlocked}
                <Lock size={12} aria-label={$translate('tasks.depsBlockedHint')} />
              {/if}
              {task.title}
            </h3>
            {#if task.description}<p>{task.description}</p>{/if}
            {#if task.dependencies.length > 0}
              <div class="dependency-list">
                <span class="dependency-label">{$translate('tasks.dependencies')}</span>
                {#each task.dependencies as dependsOnId (dependsOnId)}
                  {@const chip = dependencyChip(task.projectId, dependsOnId)}
                  <span class="dependency-chip status-{chip.status}"
                    >{#if chip.status === 'missing'}{$translate(
                        'tasks.status.missing',
                      )}{:else}{chip.label} · {taskStatusLabel(chip.status)}{/if}</span
                  >
                {/each}
              </div>
            {/if}
            {#if task.blockedReason}<code>{task.blockedReason}</code>{/if}
          </article>
        {/each}
      </div>
    {/each}
  </section>
{/if}

<style>
  .new-task-form {
    display: flex;
    flex-direction: column;
    gap: 9px;
    margin-bottom: 17px;
  }
  .new-task-row {
    display: flex;
    gap: 9px;
  }
  .new-task-row input {
    flex: 1;
    min-width: 0;
    padding: 9px 12px;
    border: 1px solid var(--line);
    border-radius: var(--r-md);
    background: var(--surface);
    color: var(--text);
  }
  .dependency-picker {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 14px;
    margin: 0;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-radius: var(--r-md);
  }
  .dependency-picker legend {
    padding: 0 4px;
    color: var(--muted);
    font-size: var(--fs-xs);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  .dependency-picker label {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: var(--fs-sm);
    color: var(--text);
  }
  .board {
    display: grid;
    grid-template-columns: repeat(6, minmax(200px, 1fr));
    gap: 12px;
    overflow-x: auto;
  }
  .board-column {
    min-height: 420px;
    padding: 12px;
    border: 1px solid var(--line);
    border-radius: var(--r-lg);
    background: color-mix(in srgb, var(--surface) 70%, transparent);
  }
  .board-column > header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 4px 5px 12px;
  }
  .board-column h2 {
    margin: 0;
    font-size: var(--fs-xs);
    text-transform: uppercase;
    letter-spacing: 0.07em;
    color: var(--text);
  }
  .board-column header span {
    color: var(--muted);
    font-size: var(--fs-xs);
  }
  .board-card {
    position: relative;
    margin-bottom: 9px;
    padding: 12px;
    border: 1px solid var(--line);
    border-radius: var(--r-md);
    background: var(--surface);
    cursor: grab;
    transition: border-color 140ms ease;
  }
  .board-card:hover {
    border-color: color-mix(in srgb, var(--accent) 18%, var(--line));
  }
  .board-card:active {
    cursor: grabbing;
  }
  .board-card.is-blocked {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--line));
  }
  .board-card.is-deps-blocked {
    border-color: var(--warning);
  }
  .board-card-top {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .priority {
    color: var(--accent);
    font-size: var(--fs-2xs);
    font-weight: 700;
  }
  .delete-task {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 3px;
    border: 1px solid transparent;
    border-radius: var(--r-sm);
    background: transparent;
    color: var(--muted);
    cursor: pointer;
  }
  .delete-task:hover {
    border-color: var(--line);
    background: var(--surface-2);
    color: var(--text);
  }
  .board-card h3 {
    display: flex;
    align-items: center;
    gap: 5px;
    margin: 7px 0;
    font-size: var(--fs-sm);
    color: var(--text);
  }
  .board-card h3 :global(svg) {
    flex-shrink: 0;
    color: var(--warning);
  }
  .dependency-list {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 6px;
    margin: 0 0 8px;
  }
  .dependency-label {
    color: var(--muted);
    font-size: var(--fs-2xs);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .dependency-chip {
    padding: 2px 7px;
    border-radius: 999px;
    border: 1px solid var(--line);
    background: var(--surface-2);
    color: var(--muted);
    font-size: var(--fs-2xs);
    white-space: nowrap;
  }
  .dependency-chip.status-completed {
    border-color: color-mix(in srgb, var(--success) 45%, var(--line));
    color: var(--success);
  }
  .dependency-chip.status-missing {
    border-color: var(--danger);
    color: var(--danger);
  }
  .board-card p {
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    overflow: hidden;
    margin: 0 0 8px;
    color: var(--muted);
    font-size: var(--fs-xs);
    line-height: 1.45;
  }
  .board-card code {
    display: block;
    padding: 6px;
    border-radius: var(--r-sm);
    background: var(--surface-2);
    color: var(--accent);
    font-size: var(--fs-2xs);
  }
  @media (max-width: 1100px) {
    .board {
      grid-template-columns: repeat(3, minmax(220px, 1fr));
    }
  }
  @media (max-width: 640px) {
    .board {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
