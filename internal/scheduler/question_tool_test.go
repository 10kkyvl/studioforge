package scheduler

import (
	"encoding/json"
	"testing"
)

// claudeToolUse builds the shape a Claude assistant message has when the model
// called a tool: the call and its arguments arrive on the same stream-json
// output StudioForge already reads, which is why the question needs no callback
// channel back from the shim.
func claudeToolUse(name string, input any) map[string]any {
	return map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "Let me check with you."},
				map[string]any{"type": "tool_use", "id": "toolu_1", "name": name, "input": input},
			},
		},
	}
}

func validQuestionInput() map[string]any {
	return map[string]any{
		"question": "Which mesh format should I use?",
		"options": []any{
			map[string]any{"label": "FBX", "description": "Standard interchange format"},
			map[string]any{"label": "OBJ", "description": "Simpler, wider tool support"},
		},
	}
}

func TestDetectQuestionToolCallReadsAValidatedCall(t *testing.T) {
	block, ok := detectQuestionToolCall(claudeToolUse("mcp__studioforge__studioforge_question", validQuestionInput()))
	if !ok {
		t.Fatal("a valid tool call must produce a question")
	}
	if block.Question != "Which mesh format should I use?" {
		t.Errorf("question=%q", block.Question)
	}
	if len(block.Options) != 2 || block.Options[0].Label != "FBX" || block.Options[1].Label != "OBJ" {
		t.Errorf("options=%+v", block.Options)
	}
	if block.Options[0].Description != "Standard interchange format" {
		t.Errorf("descriptions must survive: %+v", block.Options[0])
	}
}

// How a provider decorates tool names is its own business, so the bare name has
// to work as well as the namespaced one.
func TestDetectQuestionToolCallAcceptsNamespacedAndBareNames(t *testing.T) {
	for _, name := range []string{"studioforge_question", "mcp__studioforge__studioforge_question"} {
		if _, ok := detectQuestionToolCall(claudeToolUse(name, validQuestionInput())); !ok {
			t.Errorf("%q was not recognised", name)
		}
	}
}

func TestDetectQuestionToolCallIgnoresOtherTools(t *testing.T) {
	for _, name := range []string{"Read", "mcp__Roblox_Studio__execute_luau", "studioforge_question_extra", "question"} {
		if _, ok := detectQuestionToolCall(claudeToolUse(name, validQuestionInput())); ok {
			t.Errorf("%q must not be read as a question", name)
		}
	}
}

// The shim already refused these and told the agent why, so there is nothing to
// publish here and nothing to end the turn over — the agent can fix the
// arguments and call again.
func TestDetectQuestionToolCallRejectsContractViolations(t *testing.T) {
	for name, input := range map[string]any{
		"one option":       map[string]any{"question": "Pick", "options": []any{map[string]any{"label": "Only"}}},
		"five options":     map[string]any{"question": "Pick", "options": []any{map[string]any{"label": "a"}, map[string]any{"label": "b"}, map[string]any{"label": "c"}, map[string]any{"label": "d"}, map[string]any{"label": "e"}}},
		"duplicate labels": map[string]any{"question": "Pick", "options": []any{map[string]any{"label": "same"}, map[string]any{"label": "same"}}},
		"blank question":   map[string]any{"question": "   ", "options": []any{map[string]any{"label": "a"}, map[string]any{"label": "b"}}},
		"blank label":      map[string]any{"question": "Pick", "options": []any{map[string]any{"label": ""}, map[string]any{"label": "b"}}},
		"no options":       map[string]any{"question": "Pick"},
	} {
		if _, ok := detectQuestionToolCall(claudeToolUse("mcp__studioforge__studioforge_question", input)); ok {
			t.Errorf("%s: a question violating the contract must not reach the operator", name)
		}
	}
}

func TestDetectQuestionToolCallToleratesUnrelatedPayloads(t *testing.T) {
	for name, payload := range map[string]any{
		"nil":                nil,
		"string":             "hello",
		"no message":         map[string]any{"type": "assistant"},
		"message not a map":  map[string]any{"message": "text"},
		"content not a list": map[string]any{"message": map[string]any{"content": "text"}},
		"empty content":      map[string]any{"message": map[string]any{"content": []any{}}},
		"text only":          map[string]any{"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "hi"}}}},
		"input missing":      claudeToolUse("mcp__studioforge__studioforge_question", nil),
		"input not an object": map[string]any{"message": map[string]any{"content": []any{
			map[string]any{"type": "tool_use", "name": "studioforge_question", "input": "not an object"},
		}}},
	} {
		if _, ok := detectQuestionToolCall(payload); ok {
			t.Errorf("%s must not produce a question", name)
		}
	}
}

// Stuck-run escalation builds the fence by hand and must keep working exactly as
// it did: the tool path is an addition, not a replacement.
func TestStuckEscalationStillTravelsAsAFence(t *testing.T) {
	message := buildStuckMessage(&Job{Prompt: "Build the shop menu."}, "It has produced no output for 12m0s.", nil)
	block, ok := detectQuestion(message)
	if !ok {
		t.Fatal("the escalation fence must still parse")
	}
	if len(block.Options) != 2 || block.Options[0].Label != StuckContinueLabel || block.Options[1].Label != StuckStopLabel {
		t.Errorf("options=%+v", block.Options)
	}
	// And it must not be mistaken for a tool call, which would misreport the
	// channel it arrived on.
	var payload any
	if err := json.Unmarshal([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"x"}]}}`), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := detectQuestionToolCall(payload); ok {
		t.Error("an ordinary assistant message is not a tool call")
	}
}
