// Package questions holds the contract for a closed question put to the
// operator: what a valid one is, and how it is carried.
//
// It sits on its own because two unrelated places need the same contract and
// neither should depend on the other. OpenRouter and NVIDIA ask through
// StudioForge's in-process toolset; Claude asks through StudioForge's own MCP
// server, which the Studio adapter serves. A question that would be rejected by
// one and accepted by the other is a question that renders on one provider and
// silently vanishes on another, which is the whole failure this replaces.
package questions

import (
	"encoding/json"
	"errors"
	"strings"
)

// ToolName is the tool an agent calls to put a closed question to the operator.
// It replaces asking through a fenced block in free text, where a sentence
// before the fence or a slightly malformed body produced a turn in which the
// question silently never rendered. Here the arguments are validated before
// anything is shown, and a violation comes back as a tool error the agent can
// correct and retry.
const ToolName = "studioforge_question"

// The limits below are the same contract the text-fence parser enforces
// (internal/scheduler's detectQuestion) and the same one the operator's browser
// re-checks before drawing a card (web/src/lib/questionCard.ts). A question that
// would be dropped further down the line is refused here instead, while the
// agent is still in a position to fix it.
const (
	MaxLength            = 2000
	MinOptions           = 2
	MaxOptions           = 4
	MaxLabelLength       = 120
	MaxDescriptionLength = 600
)

// Option is one answer the operator can pick.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Question is a closed multiple-choice question for the operator.
type Question struct {
	Question string   `json:"question"`
	Options  []Option `json:"options"`
}

// Validate applies the question contract, returning the normalized question.
// Labels and the question text are trimmed, because trailing whitespace is the
// difference between two options reading as duplicates and not.
func (q Question) Validate() (Question, error) {
	text := strings.TrimSpace(q.Question)
	switch {
	case text == "":
		return Question{}, errors.New("question is required")
	case len([]rune(text)) > MaxLength:
		return Question{}, errors.New("question is too long")
	}
	if len(q.Options) < MinOptions || len(q.Options) > MaxOptions {
		return Question{}, errors.New("a question needs between 2 and 4 options; use an ordinary reply for anything open-ended")
	}
	seen := make(map[string]bool, len(q.Options))
	options := make([]Option, 0, len(q.Options))
	for _, option := range q.Options {
		label := strings.TrimSpace(option.Label)
		switch {
		case label == "":
			return Question{}, errors.New("every option needs a label")
		case len([]rune(label)) > MaxLabelLength:
			return Question{}, errors.New("option label is too long: " + label)
		case seen[label]:
			return Question{}, errors.New("two options share the label " + label + "; the operator could not tell them apart")
		case len([]rune(option.Description)) > MaxDescriptionLength:
			return Question{}, errors.New("option description is too long for " + label)
		}
		seen[label] = true
		options = append(options, Option{Label: label, Description: option.Description})
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

// Description is what the tool tells the model about itself, identical on every
// path so the tool behaves the same whichever provider is running.
const Description = "Ask the operator to choose between 2 and 4 options, for a genuine closed question you need answered before continuing. Your turn ends here and resumes with their answer, so call it last and do not ask the same thing in prose as well. Not for open-ended questions and not for confirming a decision you could make yourself."

// Schema is the tool's JSON schema. Being a schema rather than a paragraph of
// prose is the point: the arguments are checked before the operator sees
// anything, so a malformed question is a tool error the agent can retry instead
// of a card that silently never rendered.
const Schema = `{"type":"object","properties":{"question":{"type":"string","description":"The question to put to the operator. One closed question, phrased so the options below answer it."},"options":{"type":"array","minItems":2,"maxItems":4,"description":"Between 2 and 4 answers to choose between.","items":{"type":"object","properties":{"label":{"type":"string","description":"Short, distinct label shown on the button."},"description":{"type":"string","description":"Optional one-line explanation of what this choice means."}},"required":["label"]}}},"required":["question","options"]}`

// SchemaMap is Schema as the map an MCP tools/list entry carries.
func SchemaMap() map[string]any {
	var out map[string]any
	// Schema is a compile-time constant this package's own tests parse.
	_ = json.Unmarshal([]byte(Schema), &out)
	return out
}

// Accepted is what the tool returns to an agent whose question reached the
// operator: an instruction to stop, because the answer arrives as the next
// message rather than as this call's result.
func Accepted(q Question) string {
	return "Asked the operator: " + q.Question + "\nEnd your turn now; their answer arrives as the next message."
}
