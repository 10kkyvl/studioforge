package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

type Store struct{ db *DB }

func NewStore(db *DB) *Store { return &Store{db: db} }

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:])
}

func parseTime(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.SQL.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE key=?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.SQL.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, Now())
	return err
}

func (s *Store) CreateProject(ctx context.Context, project models.Project) (models.Project, error) {
	if project.ID == "" {
		project.ID = NewID()
	}
	if project.Tags == nil {
		project.Tags = make([]string, 0)
	}
	now := Now()
	if project.CreatedAt.IsZero() {
		project.CreatedAt = parseTime(now)
	}
	project.UpdatedAt = parseTime(now)
	_, err := s.db.SQL.ExecContext(ctx, `INSERT INTO projects
(id,name,canonical_path,fingerprint,description,pinned,archived,mock,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?)`, project.ID, project.Name, project.Path, project.Fingerprint, project.Description,
		boolInt(project.Pinned), boolInt(project.Archived), boolInt(project.Mock), project.CreatedAt.UTC().Format(time.RFC3339Nano), now)
	if err != nil {
		return models.Project{}, fmt.Errorf("create project: %w", err)
	}
	return project, nil
}

func (s *Store) ListProjects(ctx context.Context, includeArchived bool) ([]models.Project, error) {
	query := `SELECT p.id,p.name,p.canonical_path,p.fingerprint,p.description,COALESCE(g.name,''),p.pinned,p.archived,p.mock,p.created_at,p.updated_at,
COALESCE((SELECT SUM(b.limit_amount) FROM budgets b WHERE b.project_id=p.id AND b.scope='daily'),0),
COALESCE((SELECT SUM(u.cost) FROM usage_records u WHERE u.project_id=p.id),0),
COALESCE((SELECT COUNT(*) FROM runs r WHERE r.project_id=p.id AND r.status IN ('starting','running','waiting_resources')),0)
FROM projects p LEFT JOIN project_groups g ON g.id=p.group_id WHERE p.deleted_at IS NULL`
	if !includeArchived {
		query += " AND p.archived=0"
	}
	query += " ORDER BY p.pinned DESC,p.updated_at DESC,p.name"
	rows, err := s.db.SQL.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	var result []models.Project
	for rows.Next() {
		var p models.Project
		var pinned, archived, mock int
		var created, updated string
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.Fingerprint, &p.Description, &p.GroupName, &pinned, &archived, &mock, &created, &updated, &p.BudgetLimit, &p.BudgetUsed, &p.RunningAgents); err != nil {
			return nil, err
		}
		p.Pinned, p.Archived, p.Mock = pinned != 0, archived != 0, mock != 0
		p.CreatedAt, p.UpdatedAt = parseTime(created), parseTime(updated)
		p.Tags = make([]string, 0)
		tagRows, err := s.db.SQL.QueryContext(ctx, `SELECT t.name FROM tags t JOIN project_tags pt ON pt.tag_id=t.id WHERE pt.project_id=? ORDER BY t.name`, p.ID)
		if err != nil {
			return nil, err
		}
		for tagRows.Next() {
			var tag string
			if err := tagRows.Scan(&tag); err != nil {
				tagRows.Close()
				return nil, err
			}
			p.Tags = append(p.Tags, tag)
		}
		tagRows.Close()
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) Project(ctx context.Context, id string) (models.Project, error) {
	projects, err := s.ListProjects(ctx, true)
	if err != nil {
		return models.Project{}, err
	}
	for _, p := range projects {
		if p.ID == id {
			return p, nil
		}
	}
	return models.Project{}, sql.ErrNoRows
}

func (s *Store) SetProjectArchived(ctx context.Context, id string, archived bool) error {
	res, err := s.db.SQL.ExecContext(ctx, "UPDATE projects SET archived=?,updated_at=? WHERE id=? AND deleted_at IS NULL", boolInt(archived), Now(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListAgents(ctx context.Context, projectID string) ([]models.Agent, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT a.id,a.project_id,a.name,a.role,a.provider,a.model_alias,a.effort,a.enabled,a.permission_profile,a.concurrency,a.budget,COALESCE(t.system_prompt,'')
FROM project_agents a LEFT JOIN agent_templates t ON t.id=a.template_id WHERE (?='' OR a.project_id=?) ORDER BY a.project_id,a.name`, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Agent
	for rows.Next() {
		var a models.Agent
		var enabled int
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.Name, &a.Role, &a.Provider, &a.ModelAlias, &a.Effort, &enabled, &a.Permission, &a.Concurrency, &a.Budget, &a.SystemPrompt); err != nil {
			return nil, err
		}
		a.Enabled = enabled != 0
		if a.Permission == "safe" {
			a.Permission = "workspace-write"
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) CreateAgent(ctx context.Context, agent models.Agent) (models.Agent, error) {
	if agent.ID == "" {
		agent.ID = NewID()
	}
	if agent.Name == "" {
		agent.Name = "Default Agent"
	}
	if agent.Role == "" {
		agent.Role = "Roblox Engineer"
	}
	if agent.Provider == "" {
		agent.Provider = "codex"
	}
	if agent.ModelAlias == "" {
		agent.ModelAlias = "default"
	}
	if agent.Effort == "" {
		agent.Effort = "medium"
	}
	if agent.Permission == "" {
		agent.Permission = "workspace-write"
	}
	if agent.Concurrency <= 0 {
		agent.Concurrency = 1
	}
	if agent.Budget <= 0 {
		agent.Budget = 10
	}
	agent.Enabled = true
	_, err := s.db.SQL.ExecContext(ctx, `INSERT INTO project_agents
(id,project_id,name,role,provider,model_alias,effort,enabled,permission_profile,concurrency,budget)
VALUES(?,?,?,?,?,?,?,?,?,?,?)`, agent.ID, agent.ProjectID, agent.Name, agent.Role, agent.Provider,
		agent.ModelAlias, agent.Effort, boolInt(agent.Enabled), agent.Permission, agent.Concurrency, agent.Budget)
	if err != nil {
		return models.Agent{}, fmt.Errorf("create agent: %w", err)
	}
	return agent, nil
}

func (s *Store) EnsureDefaultAgent(ctx context.Context, projectID, provider, model, effort string) (models.Agent, bool, error) {
	agents, err := s.ListAgents(ctx, projectID)
	if err != nil {
		return models.Agent{}, false, err
	}
	if len(agents) > 0 {
		return agents[0], false, nil
	}
	agent, err := s.CreateAgent(ctx, models.Agent{ProjectID: projectID, Provider: provider, ModelAlias: model, Effort: effort})
	return agent, err == nil, err
}

func (s *Store) UpdateAgent(ctx context.Context, agent models.Agent) (models.Agent, error) {
	res, err := s.db.SQL.ExecContext(ctx, `UPDATE project_agents SET
name=?,role=?,provider=?,model_alias=?,effort=?,enabled=?,permission_profile=?,concurrency=?,budget=? WHERE id=? AND project_id=?`,
		agent.Name, agent.Role, agent.Provider, agent.ModelAlias, agent.Effort, boolInt(agent.Enabled), agent.Permission,
		agent.Concurrency, agent.Budget, agent.ID, agent.ProjectID)
	if err != nil {
		return models.Agent{}, fmt.Errorf("update agent: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return models.Agent{}, sql.ErrNoRows
	}
	return agent, nil
}

func (s *Store) ListTasks(ctx context.Context, projectID string) ([]models.Task, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT id,project_id,title,description,acceptance_criteria,priority,status,COALESCE(assigned_agent_id,''),blocked_reason
FROM tasks WHERE (?='' OR project_id=?) ORDER BY priority DESC,created_at`, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Task
	for rows.Next() {
		var t models.Task
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Description, &t.AcceptanceCriteria, &t.Priority, &t.Status, &t.AssignedAgentID, &t.BlockedReason); err != nil {
			return nil, err
		}
		deps, err := s.taskDependencies(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		t.Dependencies = deps
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) taskDependencies(ctx context.Context, taskID string) ([]string, error) {
	rows, err := s.db.SQL.QueryContext(ctx, "SELECT depends_on_task_id FROM task_dependencies WHERE task_id=? ORDER BY depends_on_task_id", taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Keep the JSON contract stable for tasks without dependencies. A nil slice
	// becomes `null`, which is surprising to TypeScript clients expecting an
	// array and previously crashed the Tasks view on `.length`.
	out := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func marshal(v any) string { b, _ := json.Marshal(v); return string(b) }
