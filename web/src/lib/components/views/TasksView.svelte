<script lang="ts">
  import { Lock, Plus, X } from '@lucide/svelte';
  import { translate, type TranslationKey } from '$lib/i18n';
  import {
    isTaskBlocked,
    isTaskReadyToStart,
    taskDependencyGraph,
    taskGraphRelations,
    taskReadiness,
  } from '$lib/tasksReadiness';
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
  let showGraph = false;
  let readyOnly = false;
  let selectedTaskId: string | null = null;

  $: visibleTasks = project ? tasks.filter((task) => task.projectId === project?.id) : tasks;
  $: if (selectedTaskId && !visibleTasks.some((task) => task.id === selectedTaskId)) {
    selectedTaskId = null;
  }

  $: readyTasks = visibleTasks.filter((task) => isTaskReadyToStart(tasks, task.id));
  $: filteredTasks = readyOnly ? readyTasks : visibleTasks;
  $: if (selectedTaskId && !filteredTasks.some((task) => task.id === selectedTaskId)) {
    selectedTaskId = null;
  }

  // Group reactively so the board re-renders when tasks change. A template that
  // called a tasksIn(column.key) helper hid the visibleTasks dependency from
  // Svelte, so created/moved/deleted tasks only appeared after a full reload.
  $: grouped = Object.fromEntries(
    columns.map((column) => [
      column.key,
      filteredTasks.filter((task) => task.status === column.key),
    ]),
  ) as Record<string, Task[]>;

  $: graph = taskDependencyGraph(filteredTasks, project?.id);
  $: graphDepths = [...new Set(graph.nodes.map((node) => node.depth))].sort((a, b) => a - b);
  $: graphWidth = Math.max(1, graphDepths.length) * 260 + 20;
  $: graphHeight =
    Math.max(
      1,
      ...graphDepths.map((depth) => graph.nodes.filter((node) => node.depth === depth).length),
    ) *
      112 +
    38;
  $: selectedRelations = selectedTaskId
    ? taskGraphRelations(tasks, selectedTaskId)
    : { blockers: [], dependents: [] };

  function taskById(taskId: string): Task | undefined {
    return tasks.find((task) => task.id === taskId);
  }

  function taskNodeState(task: Task): string {
    if (task.status === 'completed') return 'completed';
    if (task.status === 'running') return 'running';
    if (task.status === 'blocked' || isTaskBlocked(tasks, task.id)) return 'blocked';
    if (isTaskReadyToStart(tasks, task.id)) return 'ready';
    return task.status;
  }

  function isHighlighted(taskId: string): boolean {
    return (
      taskId === selectedTaskId ||
      selectedRelations.blockers.includes(taskId) ||
      selectedRelations.dependents.includes(taskId)
    );
  }

  function isEdgeHighlighted(edge: { from: string; to: string }): boolean {
    return selectedTaskId !== null && isHighlighted(edge.from) && isHighlighted(edge.to);
  }

  function toggleTaskSelection(taskId: string) {
    selectedTaskId = selectedTaskId === taskId ? null : taskId;
  }

  function clearSelection() {
    selectedTaskId = null;
  }

  function graphPosition(taskId: string): { x: number; y: number } {
    const node = graph.nodes.find((candidate) => candidate.task.id === taskId);
    if (!node) return { x: 0, y: 0 };
    const index = graph.nodes
      .filter((candidate) => candidate.depth === node.depth)
      .findIndex((candidate) => candidate.task.id === taskId);
    return { x: node.depth * 260 + 10, y: 28 + index * 112 };
  }

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

  function blockerLabel(blocker: { title: string; status: string }): string {
    return blocker.title || $translate('tasks.status.missing');
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
{#if visibleTasks.length > 0}
  <div class="tasks-toolbar" role="toolbar" aria-label={$translate('tasks.viewControls')}>
    <div class="view-switcher" role="group" aria-label={$translate('tasks.viewControls')}>
      <button
        class:active={!showGraph}
        type="button"
        data-testid="tasks-board-toggle"
        aria-pressed={!showGraph}
        onclick={() => (showGraph = false)}>{$translate('tasks.boardView')}</button
      >
      <button
        class:active={showGraph}
        type="button"
        data-testid="tasks-graph-toggle"
        aria-pressed={showGraph}
        onclick={() => (showGraph = true)}>{$translate('tasks.graphView')}</button
      >
    </div>
    <label class="ready-filter">
      <input type="checkbox" data-testid="tasks-ready-filter" bind:checked={readyOnly} />
      {$translate('tasks.readyFilter')}
    </label>
    {#if selectedTaskId}
      <button type="button" class="clear-selection" onclick={clearSelection}>
        {$translate('tasks.clearSelection')}
      </button>
    {/if}
  </div>
{/if}
{#if visibleTasks.length === 0}
  <div class="empty">
    <p>{$translate('tasks.empty')}</p>
  </div>
{:else if filteredTasks.length === 0}
  <div class="empty">
    <p>{$translate('tasks.readyEmpty')}</p>
  </div>
{:else if showGraph}
  <section
    class="dependency-graph"
    data-testid="tasks-graph"
    aria-label={$translate('tasks.graphTitle')}
  >
    <div class="graph-heading">
      <div>
        <h2>{$translate('tasks.graphTitle')}</h2>
        <p>{$translate('tasks.graphHint')}</p>
      </div>
      {#if selectedTaskId}
        {@const selectedTask = taskById(selectedTaskId)}
        {#if selectedTask}
          <p class="selection-summary">
            {$translate('tasks.selectedTask')}: <strong>{selectedTask.title}</strong>
          </p>
        {/if}
      {/if}
    </div>
    <div class="graph-legend" aria-label={$translate('tasks.nodeStates')}>
      {#each ['ready', 'running', 'blocked', 'completed'] as state}
        <span class="graph-legend-item state-{state}">{taskStatusLabel(state)}</span>
      {/each}
      {#if selectedTaskId}
        <span class="graph-legend-item relation-upstream">{$translate('tasks.dependencies')}</span>
        <span class="graph-legend-item relation-downstream">{$translate('tasks.dependents')}</span>
      {/if}
    </div>
    <div class="graph-scroll" aria-label={$translate('tasks.graphTitle')}>
      <div class="graph-canvas" style={`width: ${graphWidth}px; height: ${graphHeight}px`}>
        <svg class="graph-connections" width={graphWidth} height={graphHeight} aria-hidden="true">
          <defs>
            <marker
              id="task-graph-arrow"
              markerWidth="7"
              markerHeight="7"
              refX="6"
              refY="3.5"
              orient="auto"
            >
              <path d="M0,0 L7,3.5 L0,7 z" fill="currentColor" />
            </marker>
          </defs>
          {#each graph.edges as edge (`${edge.from}-${edge.to}`)}
            {@const from = graphPosition(edge.from)}
            {@const to = graphPosition(edge.to)}
            <path
              class="graph-connection"
              class:is-related={isEdgeHighlighted(edge)}
              class:is-dimmed={selectedTaskId !== null && !isEdgeHighlighted(edge)}
              d={`M ${from.x + 210} ${from.y + 40} C ${from.x + 240} ${from.y + 40}, ${to.x - 30} ${to.y + 40}, ${to.x} ${to.y + 40}`}
              marker-end="url(#task-graph-arrow)"
            />
          {/each}
        </svg>
        {#each graphDepths as depth (depth)}
          <span class="graph-layer-label" style={`left: ${depth * 260 + 10}px`}
            >{$translate('tasks.graphLayer')} {depth + 1}</span
          >
        {/each}
        {#each graph.nodes as node (node.task.id)}
          {@const task = node.task}
          {@const position = graphPosition(task.id)}
          <button
            type="button"
            class="graph-node state-{taskNodeState(task)}"
            class:is-selected={selectedTaskId === task.id}
            class:is-related={selectedTaskId !== null && isHighlighted(task.id)}
            class:is-upstream={selectedRelations.blockers.includes(task.id)}
            class:is-downstream={selectedRelations.dependents.includes(task.id)}
            class:is-dimmed={selectedTaskId !== null && !isHighlighted(task.id)}
            style={`left: ${position.x}px; top: ${position.y}px`}
            data-testid="task-graph-node"
            data-task-id={task.id}
            aria-pressed={selectedTaskId === task.id}
            aria-label={`${task.title} · ${taskStatusLabel(taskNodeState(task))}`}
            onclick={() => toggleTaskSelection(task.id)}
          >
            <span class="graph-node-state">{taskStatusLabel(taskNodeState(task))}</span>
            <strong>{task.title}</strong>
            {#if task.dependencies.length > 0}
              <small
                >{task.dependencies.length} {$translate('tasks.dependencies').toLowerCase()}</small
              >
            {/if}
          </button>
        {/each}
      </div>
    </div>
    {#if graph.edges.length > 0}
      <div class="graph-edges" aria-label={$translate('tasks.graphEdges')}>
        <h3>{$translate('tasks.graphEdges')}</h3>
        {#each graph.edges as edge (`${edge.from}-${edge.to}`)}
          {@const from = taskById(edge.from)}
          {@const to = taskById(edge.to)}
          {#if from && to}
            <button
              type="button"
              class="graph-edge"
              class:is-related={isEdgeHighlighted(edge)}
              class:is-dimmed={selectedTaskId !== null && !isEdgeHighlighted(edge)}
              onclick={() => toggleTaskSelection(to.id)}
            >
              <span>{from.title}</span><span aria-hidden="true">→</span><span>{to.title}</span>
            </button>
          {/if}
        {/each}
      </div>
    {:else}
      <p class="graph-no-edges">{$translate('tasks.graphNoEdges')}</p>
    {/if}
  </section>
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
          {@const readiness = taskReadiness(tasks, task.id)}
          {@const depsBlocked = !readiness.ready}
          <article
            class="board-card"
            class:is-blocked={task.status === 'blocked'}
            class:is-deps-blocked={depsBlocked}
            data-testid="task-card"
            data-task-id={task.id}
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
            {#if readiness.blockers.length > 0}
              <div class="blocker-list" data-testid="task-blocker-list">
                <span class="dependency-label">{$translate('tasks.blockedBy')}</span>
                {#each readiness.blockers as blocker (blocker.taskId)}
                  <span class="blocker-chip status-{blocker.status}" data-testid="task-blocker">
                    {blockerLabel(blocker)} · {taskStatusLabel(blocker.status)}
                  </span>
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
  .tasks-toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 9px;
    margin-bottom: 14px;
  }
  .view-switcher {
    display: inline-flex;
    overflow: hidden;
    border: 1px solid var(--line);
    border-radius: var(--r-md);
    background: var(--surface-2);
  }
  .view-switcher button,
  .clear-selection {
    padding: 7px 10px;
    border: 0;
    background: transparent;
    color: var(--muted);
    cursor: pointer;
    font-size: var(--fs-xs);
  }
  .view-switcher button + button {
    border-left: 1px solid var(--line);
  }
  .view-switcher button.active,
  .view-switcher button:hover,
  .clear-selection:hover {
    background: var(--surface-3);
    color: var(--text);
  }
  .ready-filter {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 7px 10px;
    border: 1px solid var(--line);
    border-radius: var(--r-md);
    color: var(--text);
    cursor: pointer;
    font-size: var(--fs-xs);
  }
  .ready-filter input {
    accent-color: var(--accent);
  }
  .clear-selection {
    margin-left: auto;
    border: 1px solid var(--line);
    border-radius: var(--r-md);
  }
  .dependency-graph {
    padding: 16px;
    border: 1px solid var(--line);
    border-radius: var(--r-lg);
    background: color-mix(in srgb, var(--surface) 70%, transparent);
  }
  .graph-heading {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 16px;
    margin-bottom: 10px;
  }
  .graph-heading h2,
  .graph-heading p {
    margin: 0;
  }
  .graph-heading h2 {
    font-size: var(--fs-md);
  }
  .graph-heading p {
    margin-top: 4px;
    color: var(--muted);
    font-size: var(--fs-xs);
  }
  .selection-summary {
    text-align: right;
  }
  .selection-summary strong {
    color: var(--accent);
  }
  .graph-legend {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-bottom: 8px;
  }
  .graph-legend-item {
    padding: 3px 7px;
    border: 1px solid var(--line);
    border-radius: 999px;
    font-size: var(--fs-2xs);
  }
  .graph-legend-item.state-ready,
  .graph-node.state-ready {
    --node-color: var(--success);
  }
  .graph-legend-item.state-running,
  .graph-node.state-running {
    --node-color: var(--accent);
  }
  .graph-legend-item.state-blocked,
  .graph-node.state-blocked {
    --node-color: var(--warning);
  }
  .graph-legend-item.state-completed,
  .graph-node.state-completed {
    --node-color: var(--muted);
  }
  .graph-scroll {
    overflow-x: auto;
    padding: 4px 0 8px;
  }
  .graph-canvas {
    position: relative;
    min-width: 100%;
  }
  .graph-connections {
    position: absolute;
    inset: 0;
    overflow: visible;
    color: var(--line);
    pointer-events: none;
  }
  .graph-connections > .graph-connection {
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    marker-end: url(#task-graph-arrow);
    transition:
      opacity 140ms ease,
      stroke 140ms ease;
  }
  .graph-connections > .graph-connection.is-related {
    color: var(--accent);
    stroke-width: 2.5;
  }
  .graph-connections > .graph-connection.is-dimmed {
    opacity: 0.18;
  }
  .graph-layer-label {
    position: absolute;
    top: 0;
    color: var(--muted);
    font-size: var(--fs-2xs);
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  .graph-node {
    --node-color: var(--muted);
    position: absolute;
    z-index: 1;
    display: flex;
    width: 210px;
    min-height: 80px;
    flex-direction: column;
    align-items: flex-start;
    gap: 5px;
    padding: 10px 12px;
    border: 1px solid color-mix(in srgb, var(--node-color) 58%, var(--line));
    border-left: 4px solid var(--node-color);
    border-radius: var(--r-md);
    background: var(--surface);
    color: var(--text);
    text-align: left;
    cursor: pointer;
    transition:
      opacity 140ms ease,
      border-color 140ms ease,
      box-shadow 140ms ease;
  }
  .graph-node:hover,
  .graph-node.is-selected {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 22%, transparent);
  }
  .graph-node.is-related {
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 18%, transparent);
  }
  .graph-node.is-upstream,
  .relation-upstream {
    background: color-mix(in srgb, var(--accent) 10%, var(--surface));
  }
  .graph-node.is-downstream,
  .relation-downstream {
    background: color-mix(in srgb, var(--accent-2) 12%, var(--surface));
  }
  .graph-node.is-selected {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .graph-node.is-dimmed {
    opacity: 0.3;
  }
  .graph-node-state {
    color: var(--node-color);
    font-size: var(--fs-2xs);
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }
  .graph-node strong {
    overflow: hidden;
    width: 100%;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-sm);
  }
  .graph-node small {
    color: var(--muted);
    font-size: var(--fs-2xs);
  }
  .graph-edges {
    display: grid;
    gap: 4px;
    max-width: 700px;
    margin-top: 12px;
  }
  .graph-edges h3 {
    margin: 0 0 3px;
    color: var(--muted);
    font-size: var(--fs-2xs);
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  .graph-edge {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 7px;
    border: 1px solid var(--line);
    border-radius: var(--r-sm);
    background: var(--surface-2);
    color: var(--muted);
    font-size: var(--fs-xs);
    text-align: left;
    cursor: pointer;
  }
  .graph-edge span:first-child,
  .graph-edge span:last-child {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .graph-edge span:nth-child(2) {
    color: var(--accent);
  }
  .graph-edge.is-related {
    border-color: var(--accent);
    color: var(--text);
  }
  .graph-edge.is-dimmed {
    opacity: 0.35;
  }
  .graph-no-edges {
    margin: 12px 0 0;
    color: var(--muted);
    font-size: var(--fs-xs);
  }
  .blocker-list {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 6px;
    margin: 0 0 8px;
  }
  .blocker-chip {
    padding: 2px 7px;
    border: 1px solid color-mix(in srgb, var(--warning) 48%, var(--line));
    border-radius: 999px;
    color: var(--warning);
    font-size: var(--fs-2xs);
  }
  .blocker-chip.status-missing {
    border-color: var(--danger);
    color: var(--danger);
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
