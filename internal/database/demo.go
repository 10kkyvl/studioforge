package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var standardSkills = []string{
	"Roblox Secure Remotes", "DataStore Safety", "Rojo Project Structure",
	"Responsive Roblox UI", "Mobile Performance", "Marketplace Asset Safety",
	"Playtest Protocol", "VFX Composition", "Git Safe Changes", "Release Checklist",
}

type demoProject struct{ id, name, description, tag, color string }

func (s *Store) SeedDemo(ctx context.Context, dataDir string) error {
	projects := []demoProject{
		{"demo-obby", "Skyline Obby", "A mobile-first cooperative obstacle course.", "Gameplay", "#58a6ff"},
		{"demo-tycoon", "Harbor Tycoon", "A server-authoritative logistics tycoon.", "Economy", "#a371f7"},
		{"demo-arena", "Neon Arena", "A fast round-based arena prototype.", "Prototype", "#39d98a"},
	}
	demoRoot := filepath.Join(dataDir, "demo-projects")
	if err := os.MkdirAll(demoRoot, 0o700); err != nil {
		return fmt.Errorf("create demo root: %w", err)
	}
	for _, p := range projects {
		root := filepath.Join(demoRoot, p.id)
		if err := createDemoWorkspace(root, p.name); err != nil {
			return err
		}
	}
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := Now()
	for i, p := range projects {
		root := filepath.Join(demoRoot, p.id)
		fingerprint := sha256.Sum256([]byte(filepath.Clean(root)))
		_, err = tx.ExecContext(ctx, `INSERT INTO projects(id,name,canonical_path,fingerprint,description,pinned,archived,mock,created_at,updated_at,last_opened_at)
VALUES(?,?,?,?,?, ?,0,1,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,updated_at=excluded.updated_at`, p.id, p.name, root, hex.EncodeToString(fingerprint[:]), p.description, boolInt(i == 0), now, now, now)
		if err != nil {
			return fmt.Errorf("seed project %s: %w", p.id, err)
		}
		tagID := "tag-" + p.id
		if _, err = tx.ExecContext(ctx, "INSERT INTO tags(id,name,color) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,color=excluded.color", tagID, p.tag, p.color); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO project_tags(project_id,tag_id) VALUES(?,?)", p.id, tagID); err != nil {
			return err
		}
		periodStart := time.Now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339Nano)
		periodEnd := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour).Format(time.RFC3339Nano)
		if _, err = tx.ExecContext(ctx, `INSERT INTO budgets(id,project_id,scope,scope_key,limit_amount,warning_threshold,period_start,period_end) VALUES(?,?,'daily','',25,0.8,?,?) ON CONFLICT(project_id,scope,scope_key,period_start) DO UPDATE SET limit_amount=excluded.limit_amount`, "budget-"+p.id, p.id, periodStart, periodEnd); err != nil {
			return err
		}
	}

	presets := []struct{ id, name, role, model, prompt string }{
		{"tpl-orchestrator", "Project Orchestrator", "orchestrator", "reasoning", "You are the project orchestrator for a Roblox Studio project. Plan the work, break it into concrete steps, execute or delegate, and finish with a structured handoff describing what changed, what you verified, and what remains."},
		{"tpl-engineer", "Gameplay Engineer", "writer", "balanced", "You are a Roblox gameplay engineer. Make the smallest correct change that satisfies the request, keep the place server-authoritative, and verify your work in Studio before handing off."},
		{"tpl-qa", "QA / Playtester", "reviewer", "fast", "You are a Roblox QA playtester. Reproduce, playtest, and report defects with exact steps and evidence. Do not change gameplay code unless explicitly asked."},
		{"tpl-security", "Security Reviewer", "reviewer", "reasoning", "You are a Roblox security reviewer. Look for unsafe RemoteEvents, weak trust boundaries, and data-store risks; report findings with severity and a concrete fix."},
	}
	for _, p := range presets {
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_templates(id,name,role,description,provider,model_alias,effort,system_prompt,permission_profile,max_turns,max_runtime_seconds,max_budget,concurrency) VALUES(?,?,?,'Built-in StudioForge template','mock',?,'medium',?,'safe',20,1800,10,1) ON CONFLICT(id) DO UPDATE SET name=excluded.name,model_alias=excluded.model_alias,system_prompt=excluded.system_prompt`, p.id, p.name, p.role, p.model, p.prompt)
		if err != nil {
			return err
		}
	}
	for i, name := range standardSkills {
		skillID := fmt.Sprintf("skill-%02d", i+1)
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_skills(id,name,version,instructions,checklist,examples,allowed_tools,validation_rules) VALUES(?,?,'1.0.0',?,'[]','[]','[]','[]') ON CONFLICT(id) DO UPDATE SET name=excluded.name`, skillID, name, "Apply the "+name+" checklist and record verification evidence.")
		if err != nil {
			return err
		}
	}

	roles := []struct{ suffix, name, role, template, model string }{
		{"orch", "Forge Lead", "Project Orchestrator", "tpl-orchestrator", "reasoning"},
		{"eng", "Builder", "Gameplay Engineer", "tpl-engineer", "balanced"},
		{"qa", "Verifier", "QA / Playtester", "tpl-qa", "fast"},
	}
	for _, p := range projects {
		for _, r := range roles {
			id := p.id + "-" + r.suffix
			_, err = tx.ExecContext(ctx, `INSERT INTO project_agents(id,project_id,template_id,name,role,provider,model_alias,effort,permission_profile,enabled,concurrency,budget) VALUES(?,?,?,?,?,'mock',?,'medium','workspace-write',1,1,10) ON CONFLICT(id) DO UPDATE SET name=excluded.name,role=excluded.role,model_alias=excluded.model_alias,permission_profile=excluded.permission_profile`, id, p.id, r.template, r.name, r.role, r.model)
			if err != nil {
				return err
			}
		}
	}

	for i, p := range projects {
		tasks := []struct {
			suffix, title, status string
			priority              int
		}{
			{"design", "Lock gameplay contract", "completed", 90},
			{"build", "Implement milestone slice", "running", 80},
			{"review", "Review and playtest", "blocked", 70},
		}
		for _, t := range tasks {
			blocked := ""
			if t.status == "blocked" {
				blocked = "Waiting for project:" + p.id + ":write"
			}
			taskID := p.id + "-task-" + t.suffix
			agent := p.id + "-eng"
			if t.suffix == "design" {
				agent = p.id + "-orch"
			}
			if t.suffix == "review" {
				agent = p.id + "-qa"
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO tasks(id,project_id,title,description,acceptance_criteria,priority,status,assigned_agent_id,blocked_reason,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,blocked_reason=excluded.blocked_reason,updated_at=excluded.updated_at`, taskID, p.id, t.title, "Demo task using the same task DAG as real projects.", "Structured handoff and passing verification", t.priority, t.status, agent, blocked, now, now)
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO task_dependencies(project_id,task_id,depends_on_task_id) VALUES(?,?,?)", p.id, p.id+"-task-build", p.id+"-task-design")
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO task_dependencies(project_id,task_id,depends_on_task_id) VALUES(?,?,?)", p.id, p.id+"-task-review", p.id+"-task-build")
		if err != nil {
			return err
		}

		runID := p.id + "-history"
		created := time.Now().UTC().Add(time.Duration(-3+i) * time.Hour).Format(time.RFC3339Nano)
		cost := 0.28 + float64(i)*0.19
		_, err = tx.ExecContext(ctx, `INSERT INTO runs(id,project_id,task_id,agent_id,provider,model_alias,provider_session_id,status,phase,cost,created_at,updated_at,started_at,finished_at) VALUES(?,?,?,?,'mock','balanced',?,'completed','verified',?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, runID, p.id, p.id+"-task-design", p.id+"-orch", "session-"+p.id, cost, created, created, created, created)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO usage_records(id,project_id,run_id,agent_id,provider,model_alias,input_tokens,output_tokens,cost,recorded_at) VALUES(?,?,?,?,'mock','balanced',1200,640,?,?) ON CONFLICT(id) DO NOTHING`, "usage-"+p.id, p.id, runID, p.id+"-orch", cost, created)
		if err != nil {
			return err
		}
		payload := fmt.Sprintf(`{"message":"%s milestone contract verified"}`, p.name)
		_, err = tx.ExecContext(ctx, `INSERT INTO run_events(project_id,run_id,agent_id,event_type,raw_type,payload,created_at) SELECT ?,?,?,'message','mock.message',?,? WHERE NOT EXISTS(SELECT 1 FROM run_events WHERE run_id=?)`, p.id, runID, p.id+"-orch", payload, created, runID)
		if err != nil {
			return err
		}
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO decisions(id,project_id,title,reason,proposed_action,risk,preview,status,created_at) VALUES('demo-decision','demo-tycoon','Approve economy migration','The change rewrites saved player inventory.','Run the migration only in the staging place.','high','12 keys affected; production remains blocked.','pending',?) ON CONFLICT(id) DO NOTHING`, now)
	if err != nil {
		return err
	}
	studios := []struct {
		id, project, name, place string
		active                   int
	}{{"studio-a", "demo-obby", "Studio — Skyline Obby", "100001", 1}, {"studio-b", "demo-arena", "Studio — Neon Arena", "100002", 0}}
	for _, v := range studios {
		_, err = tx.ExecContext(ctx, `INSERT INTO studio_sessions(id,project_id,instance_id,name,place_id,game_id,active,play_state,capabilities,mock,last_seen_at) VALUES(?,?,?,?,?,'90001',?,'stopped','["playtest","screenshot","scripts"]',1,?) ON CONFLICT(id) DO UPDATE SET project_id=excluded.project_id,name=excluded.name,last_seen_at=excluded.last_seen_at`, v.id, v.project, "mock-"+v.id, v.name, v.place, v.active, now)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func createDemoWorkspace(root, name string) error {
	for _, dir := range []string{filepath.Join(root, "src", "server"), filepath.Join(root, "src", "client"), filepath.Join(root, ".agent", "skills")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create demo directory: %w", err)
		}
	}
	files := map[string]string{
		filepath.Join(root, "default.project.json"):        fmt.Sprintf("{\n  \"name\": %q,\n  \"tree\": {\n    \"$className\": \"DataModel\",\n    \"ServerScriptService\": {\"$path\": \"src/server\"},\n    \"StarterPlayer\": {\"StarterPlayerScripts\": {\"$path\": \"src/client\"}}\n  }\n}\n", name),
		filepath.Join(root, ".agent", "project.yaml"):      "name: \"" + name + "\"\nmode: demo\n",
		filepath.Join(root, ".agent", "constitution.yaml"): "architecture:\n  server_authoritative: true\n  unrelated_refactors: forbidden\nsafety:\n  production_publish_requires_confirmation: true\n",
		filepath.Join(root, ".agent", "requirements.md"):   "# Demo requirements\n\nExercise concurrent scheduling, review, and playtest flows.\n",
	}
	for path, body := range files {
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return fmt.Errorf("write demo workspace: %w", err)
		}
	}
	return nil
}
