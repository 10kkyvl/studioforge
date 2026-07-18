package prompts

import (
	"strings"
	"testing"
)

func TestAssemblyOrderAndRedaction(t *testing.T) {
	input := Input{GlobalSafety: "safe", Constitution: "constitution", Requirements: "requirements", RolePrompt: "role", Skills: []string{"skill"}, Memory: []string{"memory"}, Blackboard: "board", Task: "task", Contracts: "contract", AcceptanceCriteria: "accept", Permissions: "tools", ExpectedSchema: "schema api_key=secret-value"}
	got := Assemble(input)
	order := []string{"safe", "constitution", "requirements", "role", "skill", "memory", "board", "task", "contract", "accept", "tools", "schema"}
	previous := -1
	for _, part := range order {
		index := strings.Index(got, part)
		if index <= previous {
			t.Fatalf("%q out of order", part)
		}
		previous = index
	}
	snapshot := RedactedSnapshot(input)
	if strings.Contains(snapshot, "secret-value") || !strings.Contains(snapshot, "[REDACTED]") {
		t.Fatalf("redaction failed: %s", snapshot)
	}
}
