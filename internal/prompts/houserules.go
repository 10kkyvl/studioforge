package prompts

import "strings"

// HouseRules are the standing instructions every StudioForge agent run carries,
// regardless of which persona the operator picked, which provider runs it, or
// what access it was granted.
//
// It is deliberately a plain constant with nothing substituted into it: this is
// the byte-identical head of every system prompt on every run, which is what
// makes the parts behind it cacheable at all (see ForRun for the ordering rule).
// Anything that varies per agent, per project or per run belongs in a later
// part, never in here.
//
// The rules fix failure modes seen in practice: an agent answering in English
// after the operator wrote in Russian; an agent promising to change StudioForge
// itself when its actual subject is the Roblox project it was pointed at; and
// current frontier models' defaults toward long narration between tool calls and
// quiet scope expansion, which cost more here than in a developer tool because
// the audience is Roblox creators rather than developers reading a build log.
const HouseRules = `## How you operate

- Answer in the language of the operator's own prose in their most recent message, and keep answering in it until their own prose switches. Never treat pasted code, error logs, stack traces, console output, asset or API names, or other quoted English inside their message as a language switch — that's data they're showing you, not the language they're speaking. If they write in Russian, the whole reply is in Russian — headings, summaries and handoffs included — even when everything you're reading and acting on (file contents, tool output, docs) is in English. Code, identifiers, file paths, API names and quoted log lines stay verbatim in their original form.
- Your subject is the Roblox project in your working directory: its places, scripts, assets, gameplay and Studio state. That is the only thing you plan, change or verify.
- StudioForge is the tool running you, not your workload. You cannot see or edit its source, settings or interface. Never say you will fix, patch, restart or reconfigure StudioForge.
- If the operator reports something broken in StudioForge itself — the chat, the agent list, run history, the Studio connection — tell them plainly that it is outside what you can touch, then report whatever the Roblox side shows you. Do not invent a fix you cannot make.

## Scope and length

- Deliver what was asked, at the scope it was asked at. Make routine judgment calls yourself; stop to ask only when two readings of the request lead to materially different work.
- Don't add abstractions, handling for cases that cannot happen, or tidying of nearby code that wasn't part of the request. A working change the operator did not ask for is still a change they have to review.
- Finish the whole task before reporting it done. If part of it is blocked, do the rest and say plainly which part is left and why.
- Between tool calls, write a line only when you found something, changed direction, or hit a blocker. Routine steps need no narration.
- Lead your final message with the outcome, then the detail, written for someone who was not watching you work.`

// QuestionChannel is how the recipient of a prompt can put a closed question to
// the operator. It is a property of the run's provider and of whether the
// recipient is the run's own agent at all, so it is resolved once when the
// prompt is composed rather than discovered mid-run.
type QuestionChannel int

const (
	// NoQuestions means this prompt's recipient cannot reach the operator, so
	// no question section is emitted. Subagents are the case that matters: a
	// subagent's turn ends inside its parent's run, and an operator answer
	// resumes the parent session rather than the subagent that asked, so a
	// subagent question is a capability that does not exist.
	NoQuestions QuestionChannel = iota
	// QuestionTool means the run carries the studioforge_question tool, whose
	// arguments are schema-validated before they reach the operator.
	QuestionTool
	// QuestionFence means the run has only the text-fence protocol: it must end
	// its turn with a fenced block that StudioForge parses out of free text.
	QuestionFence
)

// questionPolicy is the half of the question rules that does not depend on how
// the question is delivered: it is what the mechanism is for, and it applies
// identically to the tool and the fence.
const questionPolicy = "- Ask the operator to choose only for a genuine closed question — 2 to 4 clear options — where you truly need their pick before continuing. Never use it for an open-ended question, and never ask one just to confirm a decision you could make yourself."

// questionSection is the rules for asking the operator a closed question, in
// the form the run can actually ask one.
//
// The tool form carries no format instructions at all: the tool's own schema
// states the shape, arguments are validated before the operator ever sees them,
// and a violation comes back as a tool error the agent can retry. The fence form
// has to spend a worked example and an explicit "nothing else in the message"
// rule defending a protocol parsed out of free text, because there the failure
// mode — a sentence before the fence, a different info-string, slightly
// malformed JSON — is a question that silently never renders.
func questionSection(channel QuestionChannel) string {
	switch channel {
	case QuestionTool:
		return "## Asking closed questions\n\n" + questionPolicy + "\n" +
			"- Ask with the studioforge_question tool. Your turn ends when you call it and resumes with the operator's answer, so make it the last thing you do; don't also write the question out in prose."
	case QuestionFence:
		return "## Asking closed questions\n\n" + questionPolicy + "\n" +
			"- When you do ask, end your turn with a fenced block whose info-string is exactly `studioforge-question`, containing nothing but JSON with two fields: `question` (string) and `options` (array of `{label, description}`, description may be empty). For example:\n\n" +
			"```studioforge-question\n" +
			`{"question": "Which mesh format should I use?", "options": [{"label": "FBX", "description": "Standard interchange format"}, {"label": "OBJ", "description": "Simpler, wider tool support"}]}` + "\n" +
			"```\n\n" +
			"- Send nothing else in that message besides the fence itself — no preamble, no trailing remarks."
	}
	return ""
}

// Spec is everything ForRun needs to compose a run's system prompt. Every field
// is optional; an empty one contributes no section.
type Spec struct {
	// Persona is the agent's own stored SystemPrompt. Stable for the life of
	// the agent.
	Persona string
	// ProjectContext is the project's standing `.agent/*` files. Stable for the
	// project; changes only when the operator edits them.
	ProjectContext string
	// Memory is the memory entries selected for this run. Volatile: it is
	// searched against the operator's own message, so it differs on almost
	// every run, which is why it is a field of its own rather than something
	// callers fold into ProjectContext.
	Memory string
	// Questions is how this run can ask the operator a closed question.
	Questions QuestionChannel
	// UI asks for the Roblox interface-construction rules, which are carried
	// only by runs whose work actually touches UI.
	UI bool
}

// ForRun composes the system prompt handed to a provider.
//
// The parts are emitted from most stable to most volatile — house rules,
// question rules, persona, project context, UI rules, memory — because prompt
// caching is a prefix match: everything after the first differing byte is
// invalidated. Two runs of the same agent on the same project share a
// byte-identical prefix up to whichever part first differs between them, and
// the parts that differ most often are the ones deliberately placed last.
//
// The run's Studio access is not composed here. It is resolved after this
// prompt is built — by the scheduler for Claude, inside the agent loop for
// OpenRouter and NVIDIA — so StudioSection is called there and appended to what
// this returns. That keeps the most run-specific part of the prompt last, which
// is where the ordering rule above wants it anyway.
//
// Empty parts are skipped.
func ForRun(spec Spec) string {
	parts := []string{HouseRules}
	if section := questionSection(spec.Questions); section != "" {
		parts = append(parts, section)
	}
	if text := strings.TrimSpace(spec.Persona); text != "" {
		parts = append(parts, "## Your role\n\n"+text)
	}
	if text := strings.TrimSpace(spec.ProjectContext); text != "" {
		parts = append(parts, "## Project context\n\n"+text)
	}
	if spec.UI {
		parts = append(parts, RobloxUICore)
	}
	if text := strings.TrimSpace(spec.Memory); text != "" {
		parts = append(parts, "## Relevant project memory\n\n"+text)
	}
	return strings.Join(parts, "\n\n")
}
