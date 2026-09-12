package database

import (
	"context"
	"github.com/10kkyvl/studioforge/internal/models"
	"sort"
	"time"
)

type UsageRun struct {
	ID        string  `json:"id"`
	AgentID   string  `json:"agentId"`
	AgentName string  `json:"agentName"`
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	CreatedAt string  `json:"createdAt"`
	Cost      float64 `json:"cost"`
	Recorded  bool    `json:"recorded"`
	models.TokenUsage
}
type UsageBucket struct {
	Key  string  `json:"key"`
	Cost float64 `json:"cost"`
}
type UsageReport struct {
	Runs           []UsageRun    `json:"runs"`
	Days           []UsageBucket `json:"days"`
	Weeks          []UsageBucket `json:"weeks"`
	Total          float64       `json:"total"`
	DailyLimit     float64       `json:"dailyLimit"`
	Last24Hours    float64       `json:"last24Hours"`
	TypicalSeconds float64       `json:"typicalSeconds"`
	PaceSamples    int           `json:"paceSamples"`
}

// UsageReport reads durable accounting rows and run summaries, never verbose
// run_events. Costs follow recorded_at in UTC, matching the budget gate; tokens
// come from each run exactly once even if it has several accounting records.
func (s *Store) UsageReport(ctx context.Context, projectID string) (UsageReport, error) {
	report := UsageReport{Runs: []UsageRun{}, Days: []UsageBucket{}, Weeks: []UsageBucket{}}
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT r.id,r.agent_id,COALESCE(a.name,r.agent_id),r.provider,r.model_alias,r.created_at,
 COALESCE(u.cost,0),COALESCE(u.n,0),r.input_tokens,r.output_tokens,r.cache_read_tokens,r.cache_creation_tokens
 FROM runs r LEFT JOIN project_agents a ON a.id=r.agent_id
 LEFT JOIN (SELECT run_id,SUM(cost) AS cost,COUNT(*) AS n FROM usage_records GROUP BY run_id) u ON u.run_id=r.id
 WHERE r.project_id=? ORDER BY r.created_at DESC,r.id`, projectID)
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var run UsageRun
		var n int
		if err := rows.Scan(&run.ID, &run.AgentID, &run.AgentName, &run.Provider, &run.Model, &run.CreatedAt, &run.Cost, &n, &run.InputTokens, &run.OutputTokens, &run.CacheReadTokens, &run.CacheCreationTokens); err != nil {
			rows.Close()
			return report, err
		}
		run.Recorded = n > 0
		report.Runs = append(report.Runs, run)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return report, err
	}
	records, err := s.db.SQL.QueryContext(ctx, `SELECT recorded_at,cost FROM usage_records WHERE project_id=?`, projectID)
	if err != nil {
		return report, err
	}
	days, weeks := map[string]float64{}, map[string]float64{}
	for records.Next() {
		var at string
		var cost float64
		if err := records.Scan(&at, &cost); err != nil {
			records.Close()
			return report, err
		}
		date, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			records.Close()
			return report, err
		}
		date = date.UTC()
		days[date.Format("2006-01-02")] += cost
		monday := date.AddDate(0, 0, -(int(date.Weekday())+6)%7)
		weeks[monday.Format("2006-01-02")] += cost
		report.Total += cost
	}
	err = records.Err()
	records.Close()
	if err != nil {
		return report, err
	}
	buckets := func(values map[string]float64) []UsageBucket {
		result := make([]UsageBucket, 0, len(values))
		for k, v := range values {
			result = append(result, UsageBucket{k, v})
		}
		sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
		return result
	}
	report.Days = buckets(days)
	report.Weeks = buckets(weeks)
	_, report.DailyLimit, report.Last24Hours, err = s.BudgetAllowed(ctx, projectID, 0)
	if err != nil {
		return report, err
	}
	report.TypicalSeconds, report.PaceSamples, err = s.TypicalRunSeconds(ctx, projectID)
	return report, err
}
