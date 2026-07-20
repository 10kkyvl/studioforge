package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/10kkyvl/studioforge/internal/models"
)

// CreateTask inserts a new task, defaulting status to "backlog" and priority
// to 50 when unset, matching the tasks table's own defaults.
func (s *Store) CreateTask(ctx context.Context, task models.Task) (models.Task, error) {
	if task.ID == "" {
		task.ID = NewID()
	}
	if task.Status == "" {
		task.Status = "backlog"
	}
	if task.Priority == 0 {
		task.Priority = 50
	}
	now := Now()
	_, err := s.db.SQL.ExecContext(ctx, `INSERT INTO tasks
(id,project_id,title,description,acceptance_criteria,priority,status,assigned_agent_id,blocked_reason,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?)`, task.ID, task.ProjectID, task.Title, task.Description, task.AcceptanceCriteria,
		task.Priority, task.Status, nullText(task.AssignedAgentID), task.BlockedReason, now, now)
	if err != nil {
		return models.Task{}, fmt.Errorf("create task: %w", err)
	}
	if task.Dependencies == nil {
		task.Dependencies = make([]string, 0)
	}
	return task, nil
}

// Task loads a single task by id, including its dependency list.
func (s *Store) Task(ctx context.Context, id string) (models.Task, error) {
	row := s.db.SQL.QueryRowContext(ctx, `SELECT id,project_id,title,description,acceptance_criteria,priority,status,COALESCE(assigned_agent_id,''),blocked_reason
FROM tasks WHERE id=?`, id)
	var t models.Task
	if err := row.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Description, &t.AcceptanceCriteria, &t.Priority, &t.Status, &t.AssignedAgentID, &t.BlockedReason); err != nil {
		return models.Task{}, err
	}
	deps, err := s.taskDependencies(ctx, t.ID)
	if err != nil {
		return models.Task{}, err
	}
	t.Dependencies = deps
	return t, nil
}

// UpdateTask persists a task's mutable fields (title, description,
// acceptance criteria, priority, status, blocked reason). Project ownership,
// assignment, and dependencies are not touched by this call.
func (s *Store) UpdateTask(ctx context.Context, task models.Task) (models.Task, error) {
	res, err := s.db.SQL.ExecContext(ctx, `UPDATE tasks SET
title=?,description=?,acceptance_criteria=?,priority=?,status=?,blocked_reason=?,updated_at=? WHERE id=?`,
		task.Title, task.Description, task.AcceptanceCriteria, task.Priority, task.Status, task.BlockedReason, Now(), task.ID)
	if err != nil {
		return models.Task{}, fmt.Errorf("update task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return models.Task{}, sql.ErrNoRows
	}
	return s.Task(ctx, task.ID)
}

// DeleteTask removes a task. sql.ErrNoRows is returned when it doesn't exist.
func (s *Store) DeleteTask(ctx context.Context, id string) error {
	res, err := s.db.SQL.ExecContext(ctx, "DELETE FROM tasks WHERE id=?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) AddTaskDependency(ctx context.Context, projectID, taskID, dependsOnTaskID string) error {
	_, err := s.db.SQL.ExecContext(ctx, "INSERT OR IGNORE INTO task_dependencies(project_id,task_id,depends_on_task_id) VALUES(?,?,?)", projectID, taskID, dependsOnTaskID)
	if err != nil {
		return fmt.Errorf("add task dependency: %w", err)
	}
	return nil
}

// SetTaskStatus updates only a task's status, used when a run attaches to it.
func (s *Store) SetTaskStatus(ctx context.Context, id, status string) error {
	res, err := s.db.SQL.ExecContext(ctx, "UPDATE tasks SET status=?,updated_at=? WHERE id=?", status, Now(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
