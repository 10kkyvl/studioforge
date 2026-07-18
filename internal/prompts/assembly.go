package prompts

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/10kkyvl/studioforge/internal/security"
)

type Input struct {
	GlobalSafety, Constitution, Requirements, RolePrompt, Blackboard, Task, Contracts, AcceptanceCriteria, Permissions, ExpectedSchema string
	Skills, Memory                                                                                                                     []string
}

func Assemble(in Input) string {
	sections := []struct{ title, body string }{{"Global safety policy", in.GlobalSafety}, {"Project constitution", in.Constitution}, {"Project requirements", in.Requirements}, {"Agent role", in.RolePrompt}, {"Selected skills", strings.Join(in.Skills, "\n\n")}, {"Relevant memory", strings.Join(in.Memory, "\n")}, {"Project blackboard", in.Blackboard}, {"Current task", in.Task}, {"Dependencies and contracts", in.Contracts}, {"Acceptance criteria", in.AcceptanceCriteria}, {"Tool permissions", in.Permissions}, {"Required structured result", in.ExpectedSchema}}
	var b strings.Builder
	for _, section := range sections {
		if strings.TrimSpace(section.body) == "" {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", section.title, section.body)
	}
	return strings.TrimSpace(b.String()) + "\n"
}
func RedactedSnapshot(in Input) string { return security.Redact(Assemble(in)) }

type TaskPlan struct {
	Tasks            []PlannedTask `json:"tasks"`
	EstimatedBudget  float64       `json:"estimatedBudget"`
	EstimatedMinutes int           `json:"estimatedMinutes"`
	AffectedSystems  []string      `json:"affectedSystems"`
}
type PlannedTask struct {
	ID, Title, AgentRole                        string
	Dependencies, Resources, AcceptanceCriteria []string
}
type TaskHandoff struct {
	Completed, ChangedFiles, Decisions, Remaining, Risks, Tests []string `json:",omitempty"`
}
type ReviewResult struct {
	Approved      bool     `json:"approved"`
	Findings      []string `json:"findings"`
	RequiredFixes []string `json:"requiredFixes"`
}
type PlaytestResult struct {
	Passed        bool     `json:"passed"`
	Scenarios     []string `json:"scenarios"`
	ConsoleErrors []string `json:"consoleErrors"`
	Artifacts     []string `json:"artifacts"`
	BugTasks      []string `json:"bugTasks"`
}
type DecisionRequest struct{ Title, Reason, ProposedAction, Risk, Preview string }
type ContractChange struct {
	Name, Previous, Proposed, Reason string
	AffectedTasks                    []string
}
type MemorySummary struct {
	Summary    string
	Sources    []string
	Confidence float64
}
type MilestoneReport struct {
	Title, Status                                      string
	CompletedTasks, Tests, Artifacts, KnownLimitations []string
	Cost                                               float64
}

func SchemaExample(v any) string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) }
