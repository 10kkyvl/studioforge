package openrouter

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/agenttools"
)

// questionCall is a model turn that calls studioforge_question with the given
// raw JSON arguments and nothing else.
func questionCall(arguments string) []wireChunk {
	return []wireChunk{{
		Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
			{Index: 0, ID: "call_q", Type: "function", Function: wireFunctionDelta{Name: agenttools.QuestionToolName, Arguments: arguments}},
		}}, FinishReason: "tool_calls"}},
	}}
}

// A question asked through the tool has to come out the far end as the same
// fenced block StudioForge already carries questions in — that is what the
// scheduler detects, what parks the run, and what the browser draws a card from
// both live and after a reload.
func TestAgentLoop_QuestionToolEmitsADetectableFence(t *testing.T) {
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		if call == 0 {
			return questionCall(`{"question":"Which mesh format?","options":[{"label":"FBX","description":"Standard interchange"},{"label":"OBJ"}]}`)
		}
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "should not get here"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "runq", ProjectID: "p1", WorkingDirectory: t.TempDir(), Prompt: "pick a format", Model: "test-model"}

	events, result := runProvider(t, provider, req)

	messages := findEvents(events, "message", "openrouter.question")
	if len(messages) != 1 {
		t.Fatalf("want exactly one question message, got %d in %+v", len(messages), events)
	}
	payload, _ := messages[0].Payload.(map[string]any)
	text := fmt.Sprint(payload["text"])
	if !strings.HasPrefix(text, "```studioforge-question\n") || !strings.HasSuffix(text, "\n```") {
		t.Fatalf("the question must arrive as a studioforge-question fence, got %q", text)
	}
	var block struct {
		Question string `json:"question"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
	}
	body := strings.TrimSuffix(strings.TrimPrefix(text, "```studioforge-question\n"), "\n```")
	if err := json.Unmarshal([]byte(body), &block); err != nil {
		t.Fatalf("fence body is not the JSON contract: %v (%q)", err, body)
	}
	if block.Question != "Which mesh format?" || len(block.Options) != 2 {
		t.Fatalf("fence body did not round-trip: %+v", block)
	}
	if block.Options[0].Label != "FBX" || block.Options[1].Label != "OBJ" {
		t.Fatalf("option labels did not survive: %+v", block.Options)
	}

	// The turn ends where the question was asked. Taking another turn would let
	// the agent answer its own question and carry on past the operator.
	if log.count() != 1 {
		t.Fatalf("the turn must end at the question; got %d HTTP calls", log.count())
	}
	if result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("asking a question is a clean end of turn, got %+v", result)
	}
}

// A question that violates the contract is a tool error the agent can act on,
// not a card that silently never renders. The run keeps going so it can correct
// itself.
//
// These are all well-formed JSON that breaks the question contract, which is
// the case this tool exists for. Arguments that are not valid JSON at all never
// reach a tool: the loop rejects the whole provider response first, which
// TestAgentLoop_MalformedToolArguments already pins.
func TestAgentLoop_MalformedQuestionIsAToolErrorNotADroppedCard(t *testing.T) {
	for name, arguments := range map[string]string{
		"one option":       `{"question":"Yes or no?","options":[{"label":"Yes"}]}`,
		"five options":     `{"question":"Pick","options":[{"label":"a"},{"label":"b"},{"label":"c"},{"label":"d"},{"label":"e"}]}`,
		"empty question":   `{"question":"   ","options":[{"label":"a"},{"label":"b"}]}`,
		"duplicate labels": `{"question":"Pick","options":[{"label":"a"},{"label":" a "}]}`,
		"missing label":    `{"question":"Pick","options":[{"label":"a"},{"description":"no label"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
				if call == 0 {
					return questionCall(arguments)
				}
				return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "I'll decide myself then."}, FinishReason: "stop"}}}}
			})
			provider := newTestProvider(t, srv)
			req := providers.RunRequest{RunID: "runq", ProjectID: "p1", WorkingDirectory: t.TempDir(), Prompt: "ask me", Model: "test-model"}

			events, result := runProvider(t, provider, req)

			results := findEvents(events, "tool", "tool.result")
			if len(results) != 1 {
				t.Fatalf("want one tool result, got %d", len(results))
			}
			payload, _ := results[0].Payload.(map[string]any)
			if payload["isError"] != true {
				t.Fatalf("a question that breaks the contract must come back as a tool error: %+v", payload)
			}
			if len(findEvents(events, "message", "openrouter.question")) != 0 {
				t.Fatal("a question that failed validation must never reach the operator")
			}
			// The run continues, so the agent can fix the call or decide for
			// itself rather than being stranded.
			if log.count() != 2 {
				t.Fatalf("the run should continue after a rejected question; got %d HTTP calls", log.count())
			}
			if result.ExitCode != 0 || result.Err != nil {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

// The tool is registered for every profile, including read-only: asking changes
// nothing in the project, and a read-only run reaches a fork in the road as
// readily as any other.
func TestAgentLoop_QuestionToolIsOfferedOnAReadOnlyRun(t *testing.T) {
	srv, log := newMockServer(t, func(call int, body []byte) []wireChunk {
		return []wireChunk{{Choices: []wireChoice{{Delta: wireDelta{Content: "done"}, FinishReason: "stop"}}}}
	})
	provider := newTestProvider(t, srv)
	req := providers.RunRequest{RunID: "runq", ProjectID: "p1", WorkingDirectory: t.TempDir(), Prompt: "look around", Model: "test-model", PermissionProfile: "read-only"}

	if _, result := runProvider(t, provider, req); result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if log.count() == 0 {
		t.Fatal("no request was made")
	}
	tools, _ := log.body(0)["tools"].([]any)
	found := false
	for _, tool := range tools {
		entry, _ := tool.(map[string]any)
		function, _ := entry["function"].(map[string]any)
		if function["name"] == agenttools.QuestionToolName {
			found = true
		}
	}
	if !found {
		t.Errorf("a read-only run must still be able to ask the operator; tools were %+v", tools)
	}
}
