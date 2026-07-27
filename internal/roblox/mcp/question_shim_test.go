package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/questions"
)

// Every test here serves with no Dial and no Launch at all. That is the
// assertion as much as anything below it: the questions-only server is
// registered on every Claude run, including on a machine with no Studio
// installed, so it must never reach for a launcher.
func TestQuestionsOnlyShimAdvertisesExactlyTheQuestion(t *testing.T) {
	responses := serveShim(t,
		ShimOptions{QuestionsOnly: true},
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("responses=%+v", responses)
	}
	var listed struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(responses[0].Result, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 1 {
		t.Fatalf("tools=%+v, want exactly the question", listed.Tools)
	}
	if listed.Tools[0].Name != questions.ToolName {
		t.Errorf("tool=%q, want %q", listed.Tools[0].Name, questions.ToolName)
	}
	if listed.Tools[0].InputSchema == nil {
		t.Error("the schema is the whole advantage over a text fence; it must be advertised")
	}
}

func TestQuestionsOnlyShimNamesStudioForgeNotStudio(t *testing.T) {
	responses := serveShim(t,
		ShimOptions{QuestionsOnly: true},
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if len(responses) != 1 {
		t.Fatalf("responses=%+v", responses)
	}
	var initialized struct {
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(responses[0].Result, &initialized); err != nil {
		t.Fatal(err)
	}
	if initialized.ServerInfo.Name != QuestionServerName {
		t.Errorf("serverInfo.name=%q, want %q", initialized.ServerInfo.Name, QuestionServerName)
	}
}

func TestQuestionsOnlyShimAcceptsAValidQuestion(t *testing.T) {
	responses := serveShim(t,
		ShimOptions{QuestionsOnly: true},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"studioforge_question","arguments":{"question":"Which mesh format?","options":[{"label":"FBX","description":"Standard"},{"label":"OBJ","description":"Simpler"}]}}}`)
	if len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("responses=%+v", responses)
	}
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(responses[0].Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("a valid question must not be an error: %+v", result)
	}
	if len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, "End your turn now") {
		t.Errorf("result=%+v, want the agent told to end its turn", result)
	}
}

// The point of the tool over the fence: a violation is something the agent can
// read and retry, not a card that silently never rendered.
func TestQuestionsOnlyShimReturnsSchemaViolationsAsToolErrors(t *testing.T) {
	for name, args := range map[string]string{
		"one option":       `{"question":"Pick","options":[{"label":"Only"}]}`,
		"five options":     `{"question":"Pick","options":[{"label":"a"},{"label":"b"},{"label":"c"},{"label":"d"},{"label":"e"}]}`,
		"blank label":      `{"question":"Pick","options":[{"label":"  "},{"label":"b"}]}`,
		"duplicate labels": `{"question":"Pick","options":[{"label":"same"},{"label":"same"}]}`,
		"no question":      `{"question":"","options":[{"label":"a"},{"label":"b"}]}`,
		"no options":       `{"question":"Pick"}`,
	} {
		responses := serveShim(t,
			ShimOptions{QuestionsOnly: true},
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"studioforge_question","arguments":`+args+`}}`)
		if len(responses) != 1 {
			t.Fatalf("%s: responses=%+v", name, responses)
		}
		if responses[0].Error != nil {
			t.Errorf("%s: a contract violation must be a tool error the agent can act on, not a protocol error: %+v", name, responses[0].Error)
			continue
		}
		var result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(responses[0].Result, &result); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !result.IsError {
			t.Errorf("%s: accepted a question that violates the contract", name)
		}
		if len(result.Content) == 0 || strings.TrimSpace(result.Content[0].Text) == "" {
			t.Errorf("%s: a refusal the agent cannot read is no better than a dropped card", name)
		}
	}
}

func TestQuestionsOnlyShimRefusesAnyOtherTool(t *testing.T) {
	responses := serveShim(t,
		ShimOptions{QuestionsOnly: true},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"execute_luau","arguments":{"code":"print(1)"}}}`)
	if len(responses) != 1 || responses[0].Error == nil {
		t.Fatalf("the questions-only server must not forward Studio tools: %+v", responses)
	}
}

// The validated question round-trips through the fence the scheduler and the
// browser already parse, so a question asked through the tool survives a page
// reload exactly like one asked through the fence.
func TestQuestionFenceStillRoundTripsFromTheContract(t *testing.T) {
	q := questions.Question{
		Question: "Which mesh format?",
		Options:  []questions.Option{{Label: "FBX"}, {Label: "OBJ"}},
	}
	validated, err := q.Validate()
	if err != nil {
		t.Fatal(err)
	}
	fence := validated.Fence()
	if !strings.HasPrefix(fence, "```studioforge-question\n") || !strings.HasSuffix(fence, "\n```") {
		t.Errorf("fence=%q", fence)
	}
}
