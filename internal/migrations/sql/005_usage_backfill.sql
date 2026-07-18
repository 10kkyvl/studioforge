-- usage_records only started filling on the live path with this release (see
-- SetRunUsage); every run recorded before it has its spend sitting only on
-- runs.cost/input_tokens/output_tokens. Replay it so BudgetUsed and the daily
-- budget gate see a project's real history instead of restarting from zero.
-- NOT EXISTS guards a run that already has a usage_records row -- the demo
-- seed writes one directly for its history run, and an install that already
-- carries demo data must not have that row doubled.
INSERT INTO usage_records(id,project_id,run_id,agent_id,provider,model_alias,input_tokens,output_tokens,cost,recorded_at)
SELECT 'usage-backfill-' || r.id, r.project_id, r.id, r.agent_id, r.provider, r.model_alias,
       r.input_tokens, r.output_tokens, r.cost,
       COALESCE(r.finished_at, r.updated_at, r.created_at)
FROM runs r
WHERE (r.cost > 0 OR r.input_tokens > 0 OR r.output_tokens > 0)
  AND NOT EXISTS (SELECT 1 FROM usage_records u WHERE u.run_id = r.id);
