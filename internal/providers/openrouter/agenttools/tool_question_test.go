package agenttools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// newQuestionToolSet builds a ToolSet with a caller-supplied Ask callback.
// Read-only is enough for these tests: the question tool needs no supervisor,
// and that it is registered on every profile regardless is its own test below.
func newQuestionToolSet(t *testing.T, ask AskOperator) *ToolSet {
	t.Helper()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	set, err := NewToolSet(ProfileReadOnly, Options{Workspace: ws, Ask: ask})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestQuestionToolValidCallReachesAsk(t *testing.T) {
	var called bool
	var got Question
	ask := func(ctx context.Context, q Question) error {
		called = true
		got = q
		return nil
	}
	set := newQuestionToolSet(t, ask)

	args := mustJSON(t, map[string]any{
		"question": "  Which approach?  ",
		"options": []map[string]string{
			{"label": "  Option A  ", "description": "first choice"},
			{"label": "Option B", "description": "second choice"},
		},
	})
	res := set.Execute(context.Background(), QuestionToolName, args)
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Content)
	}
	if !called {
		t.Fatal("expected Ask to be called")
	}
	if got.Question != "Which approach?" {
		t.Fatalf("expected trimmed question text, got %q", got.Question)
	}
	if len(got.Options) != 2 {
		t.Fatalf("expected 2 options, got %d", len(got.Options))
	}
	if got.Options[0].Label != "Option A" || got.Options[1].Label != "Option B" {
		t.Fatalf("expected trimmed option labels, got %+v", got.Options)
	}
}

func TestQuestionToolContractViolationsReturnErrorWithoutAsking(t *testing.T) {
	longLabel := strings.Repeat("a", MaxOptionLabelLength+1)
	longQuestion := strings.Repeat("a", MaxQuestionLength+1)
	longDesc := strings.Repeat("a", MaxOptionDescLength+1)

	cases := []struct {
		name string
		args json.RawMessage
	}{
		{
			name: "empty question",
			args: mustJSON(t, map[string]any{
				"question": "",
				"options":  []map[string]string{{"label": "Yes"}, {"label": "No"}},
			}),
		},
		{
			name: "1 option",
			args: mustJSON(t, map[string]any{
				"question": "Continue?",
				"options":  []map[string]string{{"label": "Yes"}},
			}),
		},
		{
			name: "5 options",
			args: mustJSON(t, map[string]any{
				"question": "Pick one",
				"options": []map[string]string{
					{"label": "A"}, {"label": "B"}, {"label": "C"}, {"label": "D"}, {"label": "E"},
				},
			}),
		},
		{
			name: "option with empty label",
			args: mustJSON(t, map[string]any{
				"question": "Continue?",
				"options":  []map[string]string{{"label": "Yes"}, {"label": ""}},
			}),
		},
		{
			name: "two options with the same label",
			args: mustJSON(t, map[string]any{
				"question": "Continue?",
				"options":  []map[string]string{{"label": "Yes"}, {"label": "Yes"}},
			}),
		},
		{
			name: "two options with the same label differing only by whitespace",
			args: mustJSON(t, map[string]any{
				"question": "Continue?",
				"options":  []map[string]string{{"label": "Yes"}, {"label": " Yes "}},
			}),
		},
		{
			name: "option label over MaxOptionLabelLength",
			args: mustJSON(t, map[string]any{
				"question": "Continue?",
				"options":  []map[string]string{{"label": "Yes"}, {"label": longLabel}},
			}),
		},
		{
			name: "question over MaxQuestionLength",
			args: mustJSON(t, map[string]any{
				"question": longQuestion,
				"options":  []map[string]string{{"label": "Yes"}, {"label": "No"}},
			}),
		},
		{
			name: "option description over MaxOptionDescLength",
			args: mustJSON(t, map[string]any{
				"question": "Continue?",
				"options": []map[string]string{
					{"label": "Yes", "description": longDesc},
					{"label": "No"},
				},
			}),
		},
		{
			name: "malformed JSON arguments",
			args: json.RawMessage(`{"question": "Continue?", "options": [`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			ask := func(ctx context.Context, q Question) error {
				called = true
				return nil
			}
			set := newQuestionToolSet(t, ask)
			res := set.Execute(context.Background(), QuestionToolName, tc.args)
			if !res.IsError {
				t.Fatalf("expected IsError, got %+v", res)
			}
			if called {
				t.Fatal("expected Ask not to be called")
			}
		})
	}
}

func TestQuestionToolNilAskReturnsErrorWithoutPanic(t *testing.T) {
	set := newQuestionToolSet(t, nil)
	if !set.Has(QuestionToolName) {
		t.Fatal("expected the question tool to still be registered with a nil Ask")
	}
	args := mustJSON(t, map[string]any{
		"question": "Continue?",
		"options":  []map[string]string{{"label": "Yes"}, {"label": "No"}},
	})
	res := set.Execute(context.Background(), QuestionToolName, args)
	if !res.IsError {
		t.Fatalf("expected IsError with a nil Ask, got %+v", res)
	}
}

func TestQuestionToolAskErrorSurfacesAsToolError(t *testing.T) {
	ask := func(ctx context.Context, q Question) error {
		return errors.New("operator stream is gone")
	}
	set := newQuestionToolSet(t, ask)
	args := mustJSON(t, map[string]any{
		"question": "Continue?",
		"options":  []map[string]string{{"label": "Yes"}, {"label": "No"}},
	})
	res := set.Execute(context.Background(), QuestionToolName, args)
	if !res.IsError {
		t.Fatalf("expected IsError when Ask fails, got %+v", res)
	}
	if !strings.Contains(res.Content, "operator stream is gone") {
		t.Fatalf("expected the error to surface the underlying cause, got %q", res.Content)
	}
}

func TestQuestionToolRegisteredForEveryProfile(t *testing.T) {
	for _, profile := range []Profile{ProfileReadOnly, ProfileWorkspace, ProfileDanger} {
		t.Run(string(profile), func(t *testing.T) {
			set, _ := newTestToolSet(t, profile)
			if !set.Has(QuestionToolName) {
				t.Fatalf("profile %s: expected %s to be registered", profile, QuestionToolName)
			}
			var found bool
			for _, def := range set.Definitions() {
				if def.Function.Name != QuestionToolName {
					continue
				}
				found = true
				if def.Function.Description == "" {
					t.Fatalf("profile %s: expected %s to have a non-empty description", profile, QuestionToolName)
				}
				if !json.Valid(def.Function.Parameters) {
					t.Fatalf("profile %s: expected a valid JSON schema, got %s", profile, def.Function.Parameters)
				}
			}
			if !found {
				t.Fatalf("profile %s: expected %s among Definitions(), got %v", profile, QuestionToolName, set.Names())
			}
		})
	}
}

func TestQuestionFenceRoundTripsThroughSchedulerContract(t *testing.T) {
	q := Question{
		Question: "Continue with the migration?",
		Options: []QuestionOption{
			{Label: "Yes", Description: "proceed now"},
			{Label: "No", Description: "hold off"},
		},
	}
	fence := q.Fence()

	const prefix = "```studioforge-question\n"
	const suffix = "\n```"
	if !strings.HasPrefix(fence, prefix) {
		t.Fatalf("expected fence to start with %q, got %q", prefix, fence)
	}
	if !strings.HasSuffix(fence, suffix) {
		t.Fatalf("expected fence to end with %q, got %q", suffix, fence)
	}

	body := strings.TrimSuffix(strings.TrimPrefix(fence, prefix), suffix)
	if strings.Contains(body, "\n") {
		t.Fatalf("expected a single-line JSON body so the scheduler's regex matches, got %q", body)
	}

	var decoded Question
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("fence body did not round-trip as JSON: %v", err)
	}
	if decoded.Question != q.Question {
		t.Fatalf("expected question %q, got %q", q.Question, decoded.Question)
	}
	if len(decoded.Options) != len(q.Options) {
		t.Fatalf("expected %d options, got %d", len(q.Options), len(decoded.Options))
	}
	for i, opt := range q.Options {
		if decoded.Options[i] != opt {
			t.Fatalf("option %d mismatch: got %+v, want %+v", i, decoded.Options[i], opt)
		}
	}
}
