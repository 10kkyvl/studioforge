package agenttools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// QuestionToolName is the tool an agent calls to put a closed question to the
// operator. It replaces asking through a fenced block in free text, where a
// sentence before the fence or a slightly malformed body produced a turn in
// which the question silently never rendered. Here the arguments are validated
// before anything is shown, and a violation comes back as a tool error the agent
// can correct and retry.
const QuestionToolName = "studioforge_question"

// The limits below are the same contract the text-fence parser enforces
// (internal/scheduler's detectQuestion) and the same one the operator's browser
// re-checks before drawing a card (web/src/lib/questionCard.ts). A question that
// would be dropped further down the line is refused here instead, while the
// agent is still in a position to fix it.
const (
	MaxQuestionLength    = 2000
	MinQuestionOptions   = 2
	MaxQuestionOptions   = 4
	MaxOptionLabelLength = 120
	MaxOptionDescLength  = 600
)

// QuestionOption is one answer the operator can pick.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Question is a closed multiple-choice question for the operator.
type Question struct {
	Question string           `json:"question"`
	Options  []QuestionOption `json:"options"`
}

// Validate applies the question contract, returning the normalized question.
// Labels and the question text are trimmed, because trailing whitespace is the
// difference between two options reading as duplicates and not.
func (q Question) Validate() (Question, error) {
	text := strings.TrimSpace(q.Question)
	switch {
	case text == "":
		return Question{}, errors.New("question is required")
	case len([]rune(text)) > MaxQuestionLength:
		return Question{}, errors.New("question is too long")
	}
	if len(q.Options) < MinQuestionOptions || len(q.Options) > MaxQuestionOptions {
		return Question{}, errors.New("a question needs between 2 and 4 options; use an ordinary reply for anything open-ended")
	}
	seen := make(map[string]bool, len(q.Options))
	options := make([]QuestionOption, 0, len(q.Options))
	for _, option := range q.Options {
		label := strings.TrimSpace(option.Label)
		switch {
		case label == "":
			return Question{}, errors.New("every option needs a label")
		case len([]rune(label)) > MaxOptionLabelLength:
			return Question{}, errors.New("option label is too long: " + label)
		case seen[label]:
			return Question{}, errors.New("two options share the label " + label + "; the operator could not tell them apart")
		case len([]rune(option.Description)) > MaxOptionDescLength:
			return Question{}, errors.New("option description is too long for " + label)
		}
		seen[label] = true
		options = append(options, QuestionOption{Label: label, Description: option.Description})
	}
	return Question{Question: text, Options: options}, nil
}

// Fence renders a validated question as the studioforge-question fenced block
// StudioForge already carries questions in — the same contract the scheduler
// detects (detectQuestion) and the browser re-checks before drawing a card
// (web/src/lib/questionCard.ts), and the same one stuck-run escalation builds by
// hand.
//
// Writing it here rather than asking the model to is the whole point of the
// tool. The format never has to survive a model's free text, so the failure it
// used to have — a sentence before the fence, a different info-string, JSON that
// is nearly right — cannot happen; and everything downstream, including how a
// question renders after the operator reloads the page, keeps working unchanged.
func (q Question) Fence() string {
	// A Question holds nothing but strings, so this cannot fail.
	encoded, _ := json.Marshal(q)
	return "```studioforge-question\n" + string(encoded) + "\n```"
}

// AskOperator delivers a validated question and reports whether it was
// delivered. It is supplied by whatever is running the loop, because the toolset
// itself has no route to the operator: it holds a workspace, a git client and a
// process supervisor, and none of those reach the event stream.
type AskOperator func(ctx context.Context, question Question) error

// questionTool is the operator-question tool. It is registered for every
// profile, including read-only: asking changes nothing in the project, and a
// read-only run that has hit a genuine fork in the road needs the answer as much
// as any other.
func questionTool(ask AskOperator) Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"question":{"type":"string","description":"The question to put to the operator. One closed question, phrased so the options below answer it."},"options":{"type":"array","minItems":2,"maxItems":4,"description":"Between 2 and 4 answers to choose between.","items":{"type":"object","properties":{"label":{"type":"string","description":"Short, distinct label shown on the button."},"description":{"type":"string","description":"Optional one-line explanation of what this choice means."}},"required":["label"]}}},"required":["question","options"]}`)
	return &funcTool{
		name:        QuestionToolName,
		description: "Ask the operator to choose between 2 and 4 options, for a genuine closed question you need answered before continuing. Your turn ends here and resumes with their answer, so call it last and do not ask the same thing in prose as well. Not for open-ended questions and not for confirming a decision you could make yourself.",
		schema:      schema,
		exec: func(ctx context.Context, raw json.RawMessage) Result {
			var q Question
			if err := parseArgs(raw, &q); err != nil {
				return errResult("invalid arguments: %v", err)
			}
			validated, err := q.Validate()
			if err != nil {
				return errResult("%v", err)
			}
			if ask == nil {
				return errResult("this run cannot reach the operator; decide and continue, and say in your reply what you chose and why")
			}
			if err := ask(ctx, validated); err != nil {
				return errResult("could not reach the operator: %v", err)
			}
			return Result{Content: "Asked the operator: " + validated.Question + "\nEnd your turn now; their answer arrives as the next message."}
		},
	}
}
