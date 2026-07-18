<script lang="ts">
  import { Plus, X } from '@lucide/svelte';
  import { translate, type TranslationKey } from '$lib/i18n';
  import type { Project, Task } from '$lib/types';

  export let tasks: Task[] = [];
  export let project: Project | undefined = undefined;
  export let onCreateTask: (t: { title: string; status: string }) => void = () => {};
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
    onCreateTask({ title, status: 'backlog' });
    newTaskTitle = '';
    showNewTask = false;
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
</script>

<section class="page-heading" data-testid="tasks-view">
  <div>
    <p class="eyebrow">{$translate('nav.tasks')}</p>
    <h1>{$translate('tasks.title')}</h1>
    <p>{$translate('tasks.subtitle')}</p>
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
    <input bind:value={newTaskTitle} placeholder={$translate('tasks.titlePlaceholder')} required />
    <button class="primary" type="submit"><Plus size={15} />{$translate('tasks.new')}</button>
  </form>
{/if}
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
        <article
          class="board-card"
          class:is-blocked={task.status === 'blocked'}
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
          <h3>{task.title}</h3>
          {#if task.description}<p>{task.description}</p>{/if}
          {#if task.blockedReason}<code>{task.blockedReason}</code>{/if}
        </article>
      {/each}
    </div>
  {/each}
</section>

<style>
  .new-task-form {
    display: flex;
    gap: 9px;
    margin-bottom: 17px;
  }
  .new-task-form input {
    flex: 1;
    min-width: 0;
    padding: 9px 12px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--surface);
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
    padding: 10px;
    border: 1px solid var(--line);
    border-radius: 10px;
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
    font-size: 0.76rem;
    text-transform: uppercase;
    letter-spacing: 0.07em;
    color: var(--text);
  }
  .board-column header span {
    color: var(--muted);
    font-size: 0.72rem;
  }
  .board-card {
    position: relative;
    margin-bottom: 9px;
    padding: 13px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--surface);
    cursor: grab;
  }
  .board-card:active {
    cursor: grabbing;
  }
  .board-card.is-blocked {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--line));
  }
  .board-card-top {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .priority {
    color: var(--accent);
    font-size: 0.64rem;
    font-weight: 750;
  }
  .delete-task {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 3px;
    border: 1px solid transparent;
    border-radius: 6px;
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
    margin: 7px 0;
    font-size: 0.82rem;
    color: var(--text);
  }
  .board-card p {
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    overflow: hidden;
    margin: 0 0 8px;
    color: var(--muted);
    font-size: 0.7rem;
    line-height: 1.45;
  }
  .board-card code {
    display: block;
    padding: 6px;
    border-radius: 5px;
    background: var(--surface-2);
    color: var(--accent);
    font-size: 0.68rem;
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
