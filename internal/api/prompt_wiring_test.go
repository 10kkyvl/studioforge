package api

import (
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/memory"
	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/prompts"
)

func TestQuestionChannelFollowsWhatTheProviderCarries(t *testing.T) {
	for provider, want := range map[string]prompts.QuestionChannel{
		"openrouter": prompts.QuestionTool,
		"nvidia":     prompts.QuestionTool,
		// Claude reaches the same tool through StudioForge's own MCP server, which
		// is registered on every Claude run rather than only on the ones granted
		// Studio, so the tool can be promised here too.
		"claude": prompts.QuestionTool,
		// The mock provider's scripted demo emits the fence by construction.
		"mock": prompts.QuestionFence,
		"":     prompts.QuestionFence,
	} {
		if got := questionChannelFor(provider); got != want {
			t.Errorf("questionChannelFor(%q) = %v, want %v", provider, got, want)
		}
	}
}

// Issue #34: a subagent used to be built with an empty project context and the
// full house rules, which meant it knew nothing about the project it was
// working on while being taught a question protocol it cannot use — a
// subagent's turn ends inside its parent's run, and the operator's answer
// resumes the parent, not the subagent that asked.
func TestSubagentsCarryProjectContextButNotTheQuestionProtocol(t *testing.T) {
	lead := models.Agent{ID: "lead", Role: "Orchestrator", Enabled: true}
	helper := models.Agent{ID: "helper", Name: "Builder", Role: "Gameplay engineer", SystemPrompt: "You build gameplay systems.", Enabled: true}

	subagents := subagentsFor(lead, []models.Agent{lead, helper}, "constitution body")
	if len(subagents) != 1 {
		t.Fatalf("expected one subagent, got %d", len(subagents))
	}
	prompt := subagents[0].Prompt

	if !strings.Contains(prompt, "constitution body") {
		t.Error("a subagent doing project work needs the project's standing context")
	}
	if !strings.Contains(prompt, "You build gameplay systems.") {
		t.Error("a subagent keeps its own persona")
	}
	if !strings.Contains(prompt, prompts.HouseRules) {
		t.Error("a subagent still carries the house rules about language and scope")
	}
	for _, unwanted := range []string{"Asking closed questions", "studioforge-question", "studioforge_question"} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("a subagent must not be taught to ask the operator (%q); it would never receive the answer", unwanted)
		}
	}
}

// Memory is searched against the operator's own message, which a subagent never
// sees, so it is context selected for a question that was not put to it.
func TestSubagentsGetNoMemory(t *testing.T) {
	lead := models.Agent{ID: "lead", Role: "Orchestrator", Enabled: true}
	helper := models.Agent{ID: "helper", Name: "Builder", Role: "Gameplay engineer", Enabled: true}

	subagents := subagentsFor(lead, []models.Agent{lead, helper}, "constitution body")
	if len(subagents) != 1 {
		t.Fatalf("expected one subagent, got %d", len(subagents))
	}
	if strings.Contains(subagents[0].Prompt, "Relevant project memory") {
		t.Error("a subagent must not carry the run's memory selection")
	}
}

// memoryBlock used to write its own "## Relevant project memory" heading,
// because the block was concatenated into the project context and had to label
// itself. Now that memory is its own prompt part, prompts.ForRun writes the
// heading — and if memoryBlock keeps writing one too, every run with a memory
// hit carries the heading twice.
func TestMemoryBlockEmitsEntriesWithoutItsOwnHeading(t *testing.T) {
	block := memoryBlock([]memory.Entry{
		{Summary: "the shop uses a ProfileService store"},
		{Summary: "the lobby teleporter is server-authoritative"},
	})
	if strings.Contains(block, "Relevant project memory") {
		t.Fatalf("memoryBlock must not write the heading; prompts.ForRun owns it. Got %q", block)
	}
	for _, want := range []string{"- the shop uses a ProfileService store", "- the lobby teleporter is server-authoritative"} {
		if !strings.Contains(block, want) {
			t.Errorf("missing %q in %q", want, block)
		}
	}

	composed := prompts.ForRun(prompts.Spec{Persona: "persona", Memory: block})
	if got := strings.Count(composed, "## Relevant project memory"); got != 1 {
		t.Fatalf("the composed prompt carries the memory heading %d times, want 1:\n%s", got, composed)
	}
}

func TestMemoryBlockIsEmptyWithoutUsableEntries(t *testing.T) {
	if got := memoryBlock(nil); got != "" {
		t.Errorf("memoryBlock(nil) = %q, want empty", got)
	}
	if got := memoryBlock([]memory.Entry{{Summary: "   "}}); got != "" {
		t.Errorf("blank summaries produce no block, got %q", got)
	}
	if strings.Contains(prompts.ForRun(prompts.Spec{Persona: "persona", Memory: memoryBlock(nil)}), "Relevant project memory") {
		t.Error("a run with no memory hits must carry no memory section")
	}
}

func TestSubagentsOnlyForAnOrchestratorLead(t *testing.T) {
	lead := models.Agent{ID: "lead", Role: "Gameplay engineer", Enabled: true}
	helper := models.Agent{ID: "helper", Name: "Builder", Role: "Builder", Enabled: true}
	if got := subagentsFor(lead, []models.Agent{lead, helper}, "constitution body"); got != nil {
		t.Errorf("a non-orchestrator lead delegates to nobody, got %d subagents", len(got))
	}
}

func TestSubagentsSkipDisabledAgentsAndTheLeadItself(t *testing.T) {
	lead := models.Agent{ID: "lead", Role: "Orchestrator", Enabled: true}
	disabled := models.Agent{ID: "off", Name: "Off", Role: "Builder", Enabled: false}
	helper := models.Agent{ID: "helper", Name: "Builder", Role: "Builder", Enabled: true}

	subagents := subagentsFor(lead, []models.Agent{lead, disabled, helper}, "")
	if len(subagents) != 1 || subagents[0].Name != "Builder" {
		t.Fatalf("expected only the enabled non-lead agent, got %+v", subagents)
	}
}
