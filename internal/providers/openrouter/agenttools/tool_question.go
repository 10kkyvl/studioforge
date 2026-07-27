package agenttools

import (
	"context"
	"encoding/json"

	"github.com/10kkyvl/studioforge/internal/questions"
)

// The question contract lives in internal/questions, because Claude reaches the
// same tool through StudioForge's own MCP server rather than through this
// toolset, and a contract enforced in only one of the two would mean a question
// that renders on one provider and vanishes on another. These aliases keep the
// contract's single definition while leaving this package's callers unchanged.
type (
	Question       = questions.Question
	QuestionOption = questions.Option
)

const (
	QuestionToolName     = questions.ToolName
	MaxQuestionLength    = questions.MaxLength
	MinQuestionOptions   = questions.MinOptions
	MaxQuestionOptions   = questions.MaxOptions
	MaxOptionLabelLength = questions.MaxLabelLength
	MaxOptionDescLength  = questions.MaxDescriptionLength
)

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
	return &funcTool{
		name:        questions.ToolName,
		description: questions.Description,
		schema:      json.RawMessage(questions.Schema),
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
			return Result{Content: questions.Accepted(validated)}
		},
	}
}
